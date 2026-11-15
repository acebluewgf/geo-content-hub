package server

import (
	"database/sql"
	"net/http"
	"strings"
)

func (s *Server) handleReviewHeartbeat(w http.ResponseWriter, r *http.Request) {
	var in struct {
		WorkerID string            `json:"worker_id"`
		UIDs     []string          `json:"uids"`
		Tokens   map[string]string `json:"lease_tokens"`
		Seconds  int               `json:"lease_seconds"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, r, 400, "invalid_json", "请求体不合法", nil)
		return
	}
	if in.WorkerID == "" || len(in.UIDs) == 0 || len(in.UIDs) > 20 {
		s.writeError(w, r, 422, "validation_failed", "需提供 worker_id 和 1–20 个 uids", nil)
		return
	}
	if in.Seconds < 120 {
		in.Seconds = 600
	}
	if in.Seconds > 3600 {
		in.Seconds = 3600
	}
	for _, uid := range in.UIDs {
		token := in.Tokens[uid]
		if token == "" {
			s.writeError(w, r, 422, "lease_token_required", "每个任务均需提供对应 lease_tokens", nil)
			return
		}
		var id int64
		err := s.db.QueryRow(`SELECT id FROM publish_jobs WHERE uid=? AND status='succeeded' AND review_status='pending' AND review_lease_owner=? AND review_lease_token=? AND review_client_id=? AND review_lease_expires_at>UTC_TIMESTAMP() FOR UPDATE`, uid, in.WorkerID, token, clientIDOf(client(r))).Scan(&id)
		if err == sql.ErrNoRows {
			s.writeError(w, r, 409, "lease_lost", "审核租约已失效，整批续租未提交", nil)
			return
		}
		if err != nil {
			return
		}
		if _, err = s.db.Exec(`UPDATE publish_jobs SET review_lease_expires_at=DATE_ADD(UTC_TIMESTAMP(),INTERVAL ? SECOND),updated_at=UTC_TIMESTAMP() WHERE id=?`, in.Seconds, id); err != nil {
			return
		}
	}
	s.writeJSON(w, 200, map[string]any{"updated": len(in.UIDs), "worker_id": in.WorkerID})
}

func (s *Server) handleReviewReport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		WorkerID   string `json:"worker_id"`
		Token      string `json:"lease_token"`
		Status     string `json:"status"`
		Note       string `json:"note"`
		URL        string `json:"url"`
		HTTPStatus int    `json:"http_status"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, r, 400, "invalid_json", "请求体不合法", nil)
		return
	}
	if in.WorkerID == "" || in.Token == "" {
		s.writeError(w, r, 422, "lease_token_required", "worker_id 和 lease_token 必填", nil)
		return
	}
	if in.Status != "pending" && in.Status != "approved" && in.Status != "rejected" {
		s.writeError(w, r, 422, "validation_failed", "审核状态不合法", nil)
		return
	}
	if len([]rune(in.Note)) > 500 || len(in.URL) > 1000 || (in.URL != "" && !strings.HasPrefix(in.URL, "https://") && !strings.HasPrefix(in.URL, "http://")) {
		s.writeError(w, r, 422, "validation_failed", "审核说明或 URL 不合法", nil)
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
	res, err := s.db.Exec(`UPDATE publish_jobs SET review_status=?,review_note=?,review_url=?,review_checked_at=UTC_TIMESTAMP(),
 review_next_at=IF(?='pending',DATE_ADD(UTC_TIMESTAMP(),INTERVAL ? SECOND),NULL),review_lease_owner='',review_lease_expires_at=NULL,updated_at=UTC_TIMESTAMP()
 WHERE id=? AND status='succeeded' AND review_status='pending' AND review_lease_owner=? AND review_lease_token=? AND review_client_id=? AND review_lease_expires_at>UTC_TIMESTAMP()`, in.Status, in.Note, in.URL, in.Status, s.settingInt("review.interval_seconds", 21600), j.ID, in.WorkerID, in.Token, clientIDOf(client(r)))
	if err != nil {
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		s.writeError(w, r, 409, "lease_lost", "审核租约已失效或不属于当前执行者", nil)
		return
	}
	// An old publication's review must not overwrite the latest publication.
	_, err = s.db.Exec(`UPDATE articles SET review_status=?,review_note=?,review_checked_at=UTC_TIMESTAMP(),updated_at=UTC_TIMESTAMP()
 WHERE id=? AND NOT EXISTS (SELECT 1 FROM publish_jobs newer WHERE newer.article_id=? AND newer.id>? AND newer.status='succeeded')`, in.Status, in.Note, j.ArticleID, j.ArticleID, j.ID)
	if err != nil {
		return
	}
	j, err = s.jobByUID(j.UID)
	if err != nil {
		return
	}
	s.writeJSON(w, 200, map[string]any{"job": s.jobSummaryOf(j)})
}
