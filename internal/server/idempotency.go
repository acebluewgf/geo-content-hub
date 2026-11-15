package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
)

type captureWriter struct {
	header http.Header
	status int
	buf    bytes.Buffer
}

func (c *captureWriter) Header() http.Header { return c.header }

func (c *captureWriter) WriteHeader(code int) { c.status = code }

func (c *captureWriter) Write(b []byte) (int, error) { return c.buf.Write(b) }

// idempotent replays a stored response for the same Idempotency-Key and rejects a different body.
func (s *Server) idempotent(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := client(r)
		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			s.writeError(w, r, 428, "idempotency_key_required", "此操作需提供 Idempotency-Key", nil)
			return
		}
		if len(key) > 128 {
			s.writeError(w, r, 400, "invalid_idempotency_key", "Idempotency-Key 最多 128 字符", nil)
			return
		}
		if c == nil {
			u := user(r)
			if u == nil {
				s.writeError(w, r, 401, "unauthorized", "请先登录", nil)
				return
			}
			c = &apiClient{ID: 0}
			key = tokenDigest(u.UID + "\n" + key)
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, (2<<20)+1))
		if err != nil {
			s.writeError(w, r, 400, "invalid_body", "读取请求体失败", nil)
			return
		}
		if len(body) > 2<<20 {
			s.writeError(w, r, 413, "too_large", "请求体最多 2 MiB", nil)
			return
		}
		if s.db.tx == nil {
			s.writeError(w, r, 500, "transaction_required", "缺少事务边界", nil)
			return
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))

		h := sha256.New()
		h.Write([]byte(r.Method + " " + r.URL.Path + "\n"))
		h.Write(body)
		hash := hex.EncodeToString(h.Sum(nil))

		var (
			code    int
			resp    string
			oldHash string
			expired bool
		)
		// The unique index serializes competing requests across processes. The
		// reservation, business changes and response are committed together.
		_, err = s.db.Exec(`INSERT INTO idempotency_keys (client_id,idem_key,endpoint,request_hash,response_code,response_body,created_at,expires_at)
		 VALUES (?,?,?,?,0,'',UTC_TIMESTAMP(),DATE_ADD(UTC_TIMESTAMP(),INTERVAL 24 HOUR))
		 ON DUPLICATE KEY UPDATE idem_key=VALUES(idem_key)`, c.ID, key, r.Method+" "+r.URL.Path, hash)
		if err != nil {
			s.writeError(w, r, 503, "idempotency_unavailable", "幂等记录暂不可用", nil)
			return
		}
		err = s.db.QueryRow(`SELECT response_code, COALESCE(response_body,''), request_hash, expires_at<=UTC_TIMESTAMP()
			FROM idempotency_keys WHERE client_id=? AND idem_key=? FOR UPDATE`, c.ID, key).
			Scan(&code, &resp, &oldHash, &expired)
		if err != nil {
			s.writeError(w, r, 503, "idempotency_unavailable", "幂等记录读取失败", nil)
			return
		}
		if !expired && code != 0 {
			if oldHash != hash {
				s.writeError(w, r, http.StatusConflict, "idempotency_mismatch", "同一 Idempotency-Key 的请求体不同", nil)
				return
			}
			w.Header().Set("Idempotent-Replay", "true")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(code)
			_, _ = w.Write([]byte(resp))
			return
		}

		cw := &captureWriter{header: http.Header{}, status: 200}
		next(cw, r)
		if cw.status < 400 && s.db.failure == nil {
			_, err = s.db.Exec(`UPDATE idempotency_keys SET endpoint=?,request_hash=?,response_code=?,response_body=?,created_at=UTC_TIMESTAMP(),expires_at=DATE_ADD(UTC_TIMESTAMP(),INTERVAL 24 HOUR) WHERE client_id=? AND idem_key=?`, r.Method+" "+r.URL.Path, hash, cw.status, cw.buf.String(), c.ID, key)
			if err != nil {
				s.writeError(w, r, 503, "idempotency_unavailable", "幂等结果保存失败，操作未提交", nil)
				return
			}
		}
		for k, vs := range cw.header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(cw.status)
		_, _ = w.Write(cw.buf.Bytes())
	}
}
