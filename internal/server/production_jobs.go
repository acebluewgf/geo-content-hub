package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleClaim(w http.ResponseWriter, r *http.Request) {
	var in struct {
		WorkerID     string `json:"worker_id"`
		Platform     string `json:"platform"`
		Limit        int    `json:"limit"`
		LeaseSeconds int    `json:"lease_seconds"`
		Protocol     int    `json:"lease_protocol"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, r, 400, "invalid_json", "请求体不合法", nil)
		return
	}
	if in.Protocol != 2 {
		s.writeError(w, r, 426, "lease_protocol_required", "发布器需升级 lease_protocol=2 并回传 lease_token", nil)
		return
	}
	in.WorkerID = strings.TrimSpace(in.WorkerID)
	in.Platform = normalizePlatform(in.Platform)
	if in.WorkerID == "" || len(in.WorkerID) > 100 {
		s.writeError(w, r, 422, "validation_failed", "worker_id 必填，最多 100 字节", nil)
		return
	}
	if in.Limit < 1 {
		in.Limit = 1
	}
	if in.Limit > 5 {
		in.Limit = 5
	}
	in.LeaseSeconds = s.leaseSeconds(in.LeaseSeconds)
	rows, err := s.db.Query(`SELECT j.id FROM publish_jobs j JOIN articles a ON a.id=j.article_id
 WHERE j.status='queued' AND j.active_key=1 AND a.deleted_at IS NULL AND a.status='ready' AND a.publish_mode='agent'
 AND (j.next_attempt_at IS NULL OR j.next_attempt_at<=UTC_TIMESTAMP()) AND (j.scheduled_at IS NULL OR j.scheduled_at<=UTC_TIMESTAMP())
 AND (j.platform=? OR ?='' OR j.platform='') ORDER BY j.id LIMIT ? FOR UPDATE`, in.Platform, in.Platform, in.Limit)
	if err != nil {
		s.writeError(w, r, 503, "queue_unavailable", "队列暂不可用", nil)
		return
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			s.db.record(err)
			break
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := []*jobDetail{}
	for _, id := range ids {
		token := newLeaseToken()
		cid := clientIDOf(client(r))
		res, err := s.db.Exec(`UPDATE publish_jobs SET status='claimed',lease_owner=?,lease_token=?,lease_client_id=?,lease_expires_at=DATE_ADD(UTC_TIMESTAMP(),INTERVAL ? SECOND),attempts=attempts+1,updated_at=UTC_TIMESTAMP() WHERE id=? AND status='queued' AND active_key=1`, in.WorkerID, token, cid, in.LeaseSeconds, id)
		if err != nil {
			return
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			continue
		}
		j, err := s.jobByUIDByID(id)
		if err != nil || j == nil {
			s.db.record(fmt.Errorf("claimed job unavailable"))
			return
		}
		a, err := s.articleByID(j.ArticleID)
		if err != nil || a == nil {
			s.db.record(fmt.Errorf("claimed article unavailable"))
			return
		}
		if !j.Snapshot.Valid {
			raw, err := json.Marshal(a)
			if err != nil {
				s.db.record(err)
				return
			}
			if _, err = s.db.Exec("UPDATE publish_jobs SET content_snapshot=? WHERE id=?", string(raw), id); err != nil {
				return
			}
			j.Snapshot = sql.NullString{String: string(raw), Valid: true}
		}
		if _, err = s.db.Exec(`UPDATE articles SET publish_status='publishing',revision=revision+1,updated_at=UTC_TIMESTAMP() WHERE id=?`, j.ArticleID); err != nil {
			return
		}
		if _, err = s.db.Exec(`INSERT INTO job_attempts (job_id,attempt_no,lease_token,client_id,worker_id,status) VALUES (?,?,?,?,?,'claimed')`, j.ID, j.Attempts, token, cid, in.WorkerID); err != nil {
			return
		}
		detail, err := s.jobDetailOf(j)
		if err != nil {
			s.db.record(err)
			return
		}
		detail.LeaseToken = token
		out = append(out, detail)
	}
	s.writeJSON(w, 200, map[string]any{"jobs": out, "lease_protocol": 2})
}
func (s *Server) leaseSeconds(seconds int) int {
	if seconds == 0 {
		seconds = s.settingInt("publish.lease_seconds", 1800)
	}
	if seconds < 300 {
		seconds = 300
	}
	if seconds > 3600 {
		seconds = 3600
	}
	return seconds
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		WorkerID string `json:"worker_id"`
		Token    string `json:"lease_token"`
		Seconds  int    `json:"lease_seconds"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, r, 400, "invalid_json", "请求体不合法", nil)
		return
	}
	if in.Token == "" || in.WorkerID == "" {
		s.writeError(w, r, 422, "lease_token_required", "worker_id 和 lease_token 必填", nil)
		return
	}
	seconds := s.leaseSeconds(in.Seconds)
	var id int64
	err := s.db.QueryRow(`SELECT id FROM publish_jobs WHERE uid=? AND status='claimed' AND lease_owner=? AND lease_token=? AND lease_client_id=? AND lease_expires_at>UTC_TIMESTAMP() FOR UPDATE`, r.PathValue("uid"), in.WorkerID, in.Token, clientIDOf(client(r))).Scan(&id)
	if err == sql.ErrNoRows {
		s.writeError(w, r, 409, "lease_lost", "租约已失效或不属于当前执行者", nil)
		return
	}
	if err != nil {
		return
	}
	// A valid heartbeat can leave the same DATETIME value within one second.
	if _, err = s.db.Exec(`UPDATE publish_jobs SET lease_expires_at=DATE_ADD(UTC_TIMESTAMP(),INTERVAL ? SECOND),updated_at=UTC_TIMESTAMP() WHERE id=?`, seconds, id); err != nil {
		return
	}
	s.writeJSON(w, 200, map[string]any{"ok": true, "lease_expires_at": time.Now().UTC().Add(time.Duration(seconds) * time.Second)})
}

type publishResult struct {
	Status       string          `json:"status"`
	WorkerID     string          `json:"worker_id"`
	Token        string          `json:"lease_token"`
	Platform     string          `json:"platform"`
	Account      string          `json:"platform_account"`
	URL          string          `json:"url"`
	PostID       string          `json:"post_id"`
	PublishedAt  *string         `json:"published_at"`
	HTTPStatus   int             `json:"http_status"`
	ErrorCode    string          `json:"error_code"`
	ErrorMessage string          `json:"error_message"`
	Meta         json.RawMessage `json:"meta"`
	NotPublished bool            `json:"not_published"`
}

func validatePublished(in *publishResult) error {
	in.Platform = normalizePlatform(in.Platform)
	in.Account = strings.TrimSpace(in.Account)
	if !platformRe.MatchString(in.Platform) || in.Account == "" || len([]rune(in.Account)) > 100 || len(in.URL) > 500 || len(in.PostID) > 100 || (!strings.HasPrefix(in.URL, "https://") && !strings.HasPrefix(in.URL, "http://")) {
		return fmt.Errorf("成功结果需包含合法 platform、platform_account 和 http(s) url")
	}
	return nil
}
func (s *Server) finishPublished(j *Job, in *publishResult) error {
	at, err := parseTimePtr(in.PublishedAt)
	if err != nil {
		return err
	}
	var date any
	if at != nil {
		date = *at
	}
	var meta any
	if len(in.Meta) > 0 {
		meta = string(in.Meta)
	}
	_, err = s.db.Exec(`UPDATE publish_jobs SET status='succeeded',active_key=NULL,lease_owner=NULL,lease_expires_at=NULL,
 platform=?,result_account=?,result_url=?,result_post_id=?,result_at=COALESCE(?,UTC_TIMESTAMP()),result_meta=?,
 last_error_code=NULL,last_error_message=NULL,review_status='pending',review_attempts=0,review_note='',review_url='',review_checked_at=NULL,
 review_next_at=DATE_ADD(UTC_TIMESTAMP(),INTERVAL ? SECOND),review_lease_owner='',review_lease_expires_at=NULL,
 finished_at=UTC_TIMESTAMP(),updated_at=UTC_TIMESTAMP() WHERE id=?`, in.Platform, in.Account, in.URL, nullStr(in.PostID), date, meta, s.settingInt("review.first_delay_seconds", 300), j.ID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE articles SET platform=?,status='ready',publish_status='published',review_status='pending',review_note='',review_checked_at=NULL,
 first_published_at=COALESCE(first_published_at,COALESCE(?,UTC_TIMESTAMP())),last_published_at=COALESCE(?,UTC_TIMESTAMP()),revision=revision+1,updated_at=UTC_TIMESTAMP() WHERE id=?`, in.Platform, date, date, j.ArticleID)
	return err
}
func (s *Server) handleResult(w http.ResponseWriter, r *http.Request) {
	var in publishResult
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, r, 400, "invalid_json", "请求体不合法", nil)
		return
	}
	if in.Status != "success" && in.Status != "failure" {
		s.writeError(w, r, 422, "validation_failed", "status 只能为 success/failure", nil)
		return
	}
	if in.Token == "" || in.WorkerID == "" {
		s.writeError(w, r, 422, "lease_token_required", "worker_id 和 lease_token 必填", nil)
		return
	}
	if len(in.ErrorCode) > 32 || len([]rune(in.ErrorMessage)) > 500 {
		s.writeError(w, r, 422, "validation_failed", "错误信息过长", nil)
		return
	}
	if in.Status == "success" {
		if err := validatePublished(&in); err != nil {
			s.writeError(w, r, 422, "validation_failed", err.Error(), nil)
			return
		}
		if _, err := parseTimePtr(in.PublishedAt); err != nil {
			s.writeError(w, r, 422, "validation_failed", "published_at 需为 RFC3339", nil)
			return
		}
	}
	j, err := s.jobByUID(r.PathValue("uid"))
	if err != nil {
		return
	}
	if j == nil {
		s.writeError(w, r, 404, "not_found", "任务不存在", nil)
		return
	}
	// Database time is authoritative, and the row remains locked until commit.
	var valid bool
	err = s.db.QueryRow(`SELECT status='claimed' AND lease_owner=? AND lease_token=? AND lease_client_id=? AND lease_expires_at>UTC_TIMESTAMP() FROM publish_jobs WHERE id=?`, in.WorkerID, in.Token, clientIDOf(client(r)), j.ID).Scan(&valid)
	if err != nil {
		return
	}
	if !valid {
		s.writeError(w, r, 409, "lease_lost", "租约已失效或不属于当前执行者", nil)
		return
	}
	state := "succeeded"
	if in.Status == "success" {
		if err = s.finishPublished(j, &in); err != nil {
			s.db.record(err)
			return
		}
	} else {
		if in.ErrorCode == "" {
			in.ErrorCode = "unknown"
		}
		pubState := "failed"
		state = "failed_permanent"
		var active any
		var next any
		if !in.NotPublished {
			state = "uncertain"
			pubState = "uncertain"
			active = 1
		} else if retryableCode(in.ErrorCode) && j.Attempts < j.MaxAttempts {
			state = "queued"
			pubState = "queued"
			active = 1
			next = time.Now().UTC().Add(time.Duration(60<<uint(minInt(j.Attempts-1, 6))) * time.Second)
		}
		_, err = s.db.Exec(`UPDATE publish_jobs SET status=?,active_key=?,lease_owner=NULL,lease_expires_at=NULL,next_attempt_at=?,last_error_code=?,last_error_message=?,finished_at=IF(?='failed_permanent',UTC_TIMESTAMP(),NULL),updated_at=UTC_TIMESTAMP() WHERE id=?`, state, active, next, in.ErrorCode, in.ErrorMessage, state, j.ID)
		if err != nil {
			return
		}
		_, err = s.db.Exec("UPDATE articles SET publish_status=?,revision=revision+1,updated_at=UTC_TIMESTAMP() WHERE id=?", pubState, j.ArticleID)
		if err != nil {
			return
		}
	}
	_, err = s.db.Exec(`UPDATE job_attempts SET status=?,error_code=?,note=?,finished_at=UTC_TIMESTAMP() WHERE job_id=? AND lease_token=?`, state, in.ErrorCode, in.ErrorMessage, j.ID, in.Token)
	if err != nil {
		return
	}
	updated, err := s.jobByUID(j.UID)
	if err != nil {
		return
	}
	s.writeJSON(w, 200, map[string]any{"job": s.jobSummaryOf(updated)})
}

func (s *Server) handleReconcile(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Resolution string `json:"resolution"`
		Note       string `json:"note"`
		Platform   string `json:"platform"`
		Account    string `json:"platform_account"`
		URL        string `json:"url"`
		PostID     string `json:"post_id"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, r, 400, "invalid_json", "请求体不合法", nil)
		return
	}
	if strings.TrimSpace(in.Note) == "" || len([]rune(in.Note)) > 500 {
		s.writeError(w, r, 422, "validation_failed", "请填写核对依据（1–500 字）", nil)
		return
	}
	j, err := s.jobByUID(r.PathValue("uid"))
	if err != nil {
		return
	}
	if j == nil {
		s.writeError(w, r, 404, "not_found", "任务不存在", nil)
		return
	}
	if j.Status != "uncertain" {
		s.writeError(w, r, 409, "invalid_state", "只有结果待核对任务可以处理", nil)
		return
	}
	state := "reconciled_not_published"
	switch in.Resolution {
	case "published":
		result := publishResult{Platform: in.Platform, Account: in.Account, URL: in.URL, PostID: in.PostID}
		if err := validatePublished(&result); err != nil {
			s.writeError(w, r, 422, "validation_failed", err.Error(), nil)
			return
		}
		if err = s.finishPublished(j, &result); err != nil {
			s.db.record(err)
			return
		}
		state = "reconciled_published"
	case "not_published":
		if _, err = s.db.Exec(`UPDATE publish_jobs SET status='failed_permanent',active_key=NULL,finished_at=UTC_TIMESTAMP(),last_error_code='confirmed_not_published',last_error_message=?,updated_at=UTC_TIMESTAMP() WHERE id=?`, in.Note, j.ID); err != nil {
			return
		}
		if _, err = s.db.Exec(`UPDATE articles SET publish_status='failed',revision=revision+1,updated_at=UTC_TIMESTAMP() WHERE id=?`, j.ArticleID); err != nil {
			return
		}
	default:
		s.writeError(w, r, 422, "validation_failed", "resolution 只能为 published/not_published", nil)
		return
	}
	if _, err = s.db.Exec(`UPDATE job_attempts SET status=?,note=?,finished_at=UTC_TIMESTAMP() WHERE job_id=? AND lease_token=?`, state, in.Note, j.ID, j.LeaseToken); err != nil {
		return
	}
	j, err = s.jobByUID(j.UID)
	if err != nil {
		return
	}
	s.writeJSON(w, 200, map[string]any{"job": s.jobSummaryOf(j)})
}

func (s *Server) StartScheduler(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				s.tickContext(ctx)
				seconds := s.settingInt("publish.tick_interval", 60)
				if seconds < 10 {
					seconds = 10
				}
				if seconds > 600 {
					seconds = 600
				}
				timer.Reset(time.Duration(seconds) * time.Second)
			}
		}
	}()
	return done
}
func (s *Server) tick() { s.tickContext(context.Background()) }
func (s *Server) tickContext(parent context.Context) {
	if !s.ticking.CompareAndSwap(false, true) {
		return
	}
	defer s.ticking.Store(false)
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	conn, err := s.db.pool.Conn(ctx)
	if err != nil {
		return
	}
	defer conn.Close()
	var schema string
	if err = conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&schema); err != nil {
		return
	}
	lockName := "contenthub.tick." + tokenDigest(schema)[:32]
	var got int
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", lockName).Scan(&got); err != nil || got != 1 {
		return
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer releaseCancel()
		var released int
		if err := conn.QueryRowContext(releaseCtx, "SELECT RELEASE_LOCK(?)", lockName).Scan(&released); err != nil || released != 1 {
			slog.Error("scheduler lock release failed", "error", err)
		}
	}()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return
	}
	defer tx.Rollback()
	local := s.scoped(ctx, tx)
	rows, err := local.db.Query(`SELECT ` + articleCols + ` FROM articles a WHERE a.status='ready' AND a.publish_mode='agent' AND a.deleted_at IS NULL AND (a.publish_status='none' OR (a.publish_status='scheduled' AND a.scheduled_at<=UTC_TIMESTAMP())) ORDER BY a.id LIMIT 20 FOR UPDATE`)
	if err != nil {
		return
	}
	for _, a := range collect(rows, scanArticle) {
		if _, err = local.enqueue(a, time.Now().UTC()); err != nil {
			local.db.record(err)
			break
		}
	}
	rows, err = local.db.Query(`SELECT ` + jobCols + ` FROM publish_jobs j WHERE j.status='claimed' AND j.lease_expires_at<=UTC_TIMESTAMP() ORDER BY j.id LIMIT 20 FOR UPDATE`)
	if err != nil {
		return
	}
	for _, j := range collect(rows, scanJob) {
		res, err := local.db.Exec(`UPDATE publish_jobs SET status='uncertain',lease_owner=NULL,lease_expires_at=NULL,last_error_code='lease_expired',last_error_message='发布结果未知，需核对平台结果',updated_at=UTC_TIMESTAMP() WHERE id=? AND status='claimed' AND lease_token=? AND lease_expires_at<=UTC_TIMESTAMP()`, j.ID, j.LeaseToken)
		if err != nil {
			break
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			continue
		}
		_, _ = local.db.Exec(`UPDATE articles SET publish_status='uncertain',revision=revision+1,updated_at=UTC_TIMESTAMP() WHERE id=?`, j.ArticleID)
		_, _ = local.db.Exec(`UPDATE job_attempts SET status='uncertain',error_code='lease_expired',finished_at=UTC_TIMESTAMP() WHERE job_id=? AND lease_token=?`, j.ID, j.LeaseToken)
	}
	_, _ = local.db.Exec("DELETE FROM idempotency_keys WHERE expires_at<UTC_TIMESTAMP() LIMIT 100")
	_, _ = local.db.Exec("DELETE FROM user_sessions WHERE expires_at<UTC_TIMESTAMP() LIMIT 100")
	_, _ = local.db.Exec(`INSERT INTO service_heartbeats(service_name,last_success_at) VALUES ('scheduler',UTC_TIMESTAMP()) ON DUPLICATE KEY UPDATE last_success_at=VALUES(last_success_at)`)
	if local.db.failure != nil {
		slog.Error("scheduler tick failed", "error", local.db.failure)
		return
	}
	if err = tx.Commit(); err != nil {
		slog.Error("scheduler commit failed", "error", err)
		return
	}
	s.lastTick.Store(time.Now().Unix())
}
func (s *Server) schedulerHealthy() bool {
	last := s.lastTick.Load()
	var shared time.Time
	if err := s.db.QueryRow("SELECT last_success_at FROM service_heartbeats WHERE service_name='scheduler'").Scan(&shared); err == nil && shared.Unix() > last {
		last = shared.Unix()
	}
	seconds := s.settingInt("publish.tick_interval", 60)
	if seconds < 10 {
		seconds = 10
	}
	if seconds > 600 {
		seconds = 600
	}
	return last > 0 && time.Since(time.Unix(last, 0)) < time.Duration(seconds*3+30)*time.Second
}
