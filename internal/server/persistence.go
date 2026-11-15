package server

import (
	"contenthub/internal/media"
	"context"
	"database/sql"
	"errors"
	"github.com/go-sql-driver/mysql"
	"net/http"
	"time"
)

// Each request has its own handle and deadline. All mutations use the same
// transaction, including idempotency results and the audit event.
type database struct {
	pool    *sql.DB
	tx      *sql.Tx
	ctx     context.Context
	failure error
}
type queryRow struct {
	row    *sql.Row
	cancel context.CancelFunc
	db     *database
}

func (r *queryRow) Scan(dest ...any) error {
	defer r.cancel()
	err := r.row.Scan(dest...)
	r.db.record(err)
	return err
}

type queryRows struct {
	*sql.Rows
	cancel context.CancelFunc
	db     *database
}

func (r *queryRows) Close() error { err := r.Rows.Close(); r.cancel(); r.db.record(err); return err }
func (r *queryRows) Next() bool {
	ok := r.Rows.Next()
	if !ok {
		r.db.record(r.Rows.Err())
		r.cancel()
	}
	return ok
}
func (d *database) record(err error) {
	if d.ctx != nil && err != nil && !errors.Is(err, sql.ErrNoRows) && d.failure == nil {
		d.failure = err
	}
}
func (d *database) deadline() (context.Context, context.CancelFunc) {
	ctx := d.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, 15*time.Second)
}
func (d *database) QueryRow(q string, args ...any) *queryRow {
	ctx, cancel := d.deadline()
	var row *sql.Row
	if d.tx != nil {
		row = d.tx.QueryRowContext(ctx, q, args...)
	} else {
		row = d.pool.QueryRowContext(ctx, q, args...)
	}
	return &queryRow{row, cancel, d}
}
func (d *database) Query(q string, args ...any) (*queryRows, error) {
	ctx, cancel := d.deadline()
	var rows *sql.Rows
	var err error
	if d.tx != nil {
		rows, err = d.tx.QueryContext(ctx, q, args...)
	} else {
		rows, err = d.pool.QueryContext(ctx, q, args...)
	}
	if err != nil {
		cancel()
		d.record(err)
		return nil, err
	}
	return &queryRows{rows, cancel, d}, nil
}
func (d *database) Exec(q string, args ...any) (sql.Result, error) {
	ctx, cancel := d.deadline()
	defer cancel()
	var res sql.Result
	var err error
	if d.tx != nil {
		res, err = d.tx.ExecContext(ctx, q, args...)
	} else {
		res, err = d.pool.ExecContext(ctx, q, args...)
	}
	d.record(err)
	return res, err
}
func (d *database) PingContext(ctx context.Context) error { return d.pool.PingContext(ctx) }
func (d *database) lock() string {
	if d.tx != nil {
		return " FOR UPDATE"
	}
	return ""
}

type domainError struct {
	status        int
	code, message string
}

func (e *domainError) Error() string { return e.message }
func (s *Server) conflict(code, message string) error {
	err := &domainError{409, code, message}
	s.db.record(err)
	return err
}

type action func(*Server, http.ResponseWriter, *http.Request)

func (s *Server) scoped(ctx context.Context, tx *sql.Tx) *Server {
	var mediaDB media.SQLStore = s.db.pool
	if tx != nil {
		mediaDB = tx
	}
	mediaStore := &media.Store{DB: mediaDB, Dir: s.cfg.MediaDir, BaseURL: s.cfg.BaseURL}
	return &Server{cfg: s.cfg, db: &database{pool: s.db.pool, tx: tx, ctx: ctx}, media: mediaStore,
		start: s.start, lastTick: s.lastTick, ticking: s.ticking, limiter: s.limiter}
}
func (s *Server) readOnly(fn action) http.HandlerFunc         { return s.endpoint(fn, false, false) }
func (s *Server) mutate(fn action) http.HandlerFunc           { return s.endpoint(fn, true, false) }
func (s *Server) mutateIdempotent(fn action) http.HandlerFunc { return s.endpoint(fn, true, true) }
func (s *Server) endpoint(fn action, write, idem bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		var tx *sql.Tx
		var err error
		if write {
			tx, err = s.db.pool.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				s.writeError(w, r, 503, "database_unavailable", "数据库暂不可用，请重试", nil)
				return
			}
			defer tx.Rollback()
		}
		local := s.scoped(ctx, tx)
		cw := &captureWriter{header: http.Header{}, status: 200}
		handler := func(w http.ResponseWriter, r *http.Request) { fn(local, w, r) }
		if idem || (write && r.Header.Get("Idempotency-Key") != "") {
			local.idempotent(handler)(cw, r)
		} else {
			handler(cw, r)
		}
		if local.db.failure != nil {
			var de *domainError
			var mysqlErr *mysql.MySQLError
			if errors.As(local.db.failure, &de) {
				s.writeError(w, r, de.status, de.code, de.message, nil)
			} else if errors.As(local.db.failure, &mysqlErr) && mysqlErr.Number == 1062 {
				s.writeError(w, r, 409, "resource_conflict", "记录已存在，请查询后重试", nil)
			} else {
				s.writeError(w, r, 503, "database_unavailable", "操作未完成，请稍后重试", local.db.failure)
			}
			return
		}
		if write && cw.status < 400 {
			if cw.header.Get("Idempotent-Replay") == "" {
				actor := "system"
				if u := user(r); u != nil {
					actor = "user:" + u.UID
				}
				if c := client(r); c != nil {
					actor = "client:" + c.UID
				}
				_, err = local.db.Exec(`INSERT INTO audit_events (actor,request_id,method,path,response_code) VALUES (?,?,?,?,?)`, actor, requestID(r), r.Method, r.URL.Path, cw.status)
				if err != nil {
					s.writeError(w, r, 503, "audit_unavailable", "操作未提交，请稍后重试", nil)
					return
				}
			}
			if err = tx.Commit(); err != nil {
				s.writeError(w, r, 503, "commit_unknown", "提交结果尚未确认，请使用同一幂等键重试或查询结果", nil)
				return
			}
		}
		for key, values := range cw.header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(cw.status)
		_, _ = w.Write(cw.buf.Bytes())
	}
}

// Drain and close before performing related queries, including within a Tx.
func collect[T any](rows *queryRows, scan func(rowScanner) (*T, error)) []*T {
	defer rows.Close()
	out := []*T{}
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			rows.db.record(err)
			return nil
		}
		out = append(out, item)
	}
	rows.db.record(rows.Err())
	return out
}
