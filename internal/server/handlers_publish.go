package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"contenthub/internal/render"
)

type jobSummary struct {
	LeaseToken       string     `json:"lease_token,omitempty"`
	UID              string     `json:"uid"`
	ArticleUID       string     `json:"article_uid"`
	Title            string     `json:"title"`
	Platform         string     `json:"platform"`
	PlatformAccount  string     `json:"platform_account,omitempty"`
	Status           string     `json:"status"`
	Attempts         int        `json:"attempts"`
	MaxAttempts      int        `json:"max_attempts"`
	ScheduledAt      *time.Time `json:"scheduled_at,omitempty"`
	NextAttemptAt    *time.Time `json:"next_attempt_at,omitempty"`
	LeaseExpiresAt   *time.Time `json:"lease_expires_at,omitempty"`
	LastErrorCode    string     `json:"last_error_code,omitempty"`
	LastErrorMessage string     `json:"last_error_message,omitempty"`
	ResultURL        string     `json:"result_url,omitempty"`
	ResultAccount    string     `json:"result_account,omitempty"`
	ResultPostID     string     `json:"result_post_id,omitempty"`
	ReviewStatus     string     `json:"review_status"`
	ReviewNote       string     `json:"review_note,omitempty"`
	ReviewURL        string     `json:"review_url,omitempty"`
	ReviewAttempts   int        `json:"review_attempts"`
	ReviewCheckedAt  *time.Time `json:"review_checked_at,omitempty"`
	ReviewNextAt     *time.Time `json:"review_next_at,omitempty"`
	ReviewLeaseUntil *time.Time `json:"review_lease_expires_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type jobDetail struct {
	SnapshotAvailable bool          `json:"snapshot_available"`
	AttemptHistory    []attemptJSON `json:"attempt_history,omitempty"`
	jobSummary
	Language     string      `json:"language"`
	Topic        string      `json:"topic,omitempty"`
	Tags         []string    `json:"tags"`
	Summary      string      `json:"summary"`
	BodyMD       string      `json:"body_md"`
	BodyHTML     string      `json:"body_html"`
	BodyText     string      `json:"body_text"`
	Cover        *mediaJSON  `json:"cover,omitempty"`
	Images       []mediaJSON `json:"images"`
	ResultPostID string      `json:"result_post_id,omitempty"`
	ResultAt     *time.Time  `json:"result_at,omitempty"`
	ResultMeta   string      `json:"result_meta,omitempty"`
	JobPayload   interface{} `json:"-"`
}

type attemptJSON struct {
	Attempt    int        `json:"attempt"`
	WorkerID   string     `json:"worker_id"`
	Status     string     `json:"status"`
	Note       string     `json:"note"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func (s *Server) jobSummaryOf(j *Job) *jobSummary {
	out := &jobSummary{
		UID: j.UID, Platform: j.Platform, Status: j.Status, Attempts: j.Attempts, MaxAttempts: j.MaxAttempts,
		ScheduledAt: timePtr(j.ScheduledAt), NextAttemptAt: timePtr(j.NextAttemptAt),
		LeaseExpiresAt: timePtr(j.LeaseExpiresAt),
		LastErrorCode:  strOr(j.LastErrorCode), LastErrorMessage: strOr(j.LastErrorMessage),
		ResultURL: strOr(j.ResultURL), ResultAccount: strOr(j.ResultAccount),
		ResultPostID: strOr(j.ResultPostID),
		ReviewStatus: j.ReviewStatus, ReviewNote: j.ReviewNote, ReviewURL: j.ReviewURL,
		ReviewAttempts: j.ReviewAttempts, ReviewCheckedAt: timePtr(j.ReviewCheckedAt),
		ReviewNextAt:     timePtr(j.ReviewNextAt),
		ReviewLeaseUntil: timePtr(j.ReviewLeaseAt),
		FinishedAt:       timePtr(j.FinishedAt), CreatedAt: j.CreatedAt.UTC(), UpdatedAt: j.UpdatedAt.UTC(),
	}
	if a, _ := s.jobArticle(j); a != nil {
		out.ArticleUID = a.UID
		out.Title = a.Title
		out.PlatformAccount = strOr(a.PlatformAccount)
	}
	return out
}

func (s *Server) jobDetailOf(j *Job) (*jobDetail, error) {
	a, err := s.jobArticle(j)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, errNotFound
	}
	d := &jobDetail{
		SnapshotAvailable: j.Snapshot.Valid, jobSummary: *s.jobSummaryOf(j),
		Language: a.Language, Topic: strOr(a.Topic), Tags: a.tags(), Summary: a.Summary,
		BodyMD: a.BodyMD, BodyHTML: a.BodyHTML, BodyText: a.BodyText,
		ResultPostID: strOr(j.ResultPostID), ResultAt: timePtr(j.ResultAt), ResultMeta: strOr(j.ResultMeta),
	}
	d.PlatformAccount = strOr(a.PlatformAccount)
	rows, err := s.db.Query(`SELECT attempt_no,worker_id,status,note,started_at,finished_at FROM job_attempts WHERE job_id=? ORDER BY attempt_no DESC LIMIT 100`, j.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var attempt attemptJSON
		var finished sql.NullTime
		if err := rows.Scan(&attempt.Attempt, &attempt.WorkerID, &attempt.Status, &attempt.Note, &attempt.StartedAt, &finished); err != nil {
			rows.Close()
			return nil, err
		}
		attempt.FinishedAt = timePtr(finished)
		d.AttemptHistory = append(d.AttemptHistory, attempt)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if !j.Snapshot.Valid {
		d.BodyMD = ""
		d.BodyHTML = ""
		d.BodyText = ""
		d.Summary = ""
		return d, nil
	}
	if a.CoverMediaID.Valid {
		if m, err := s.mediaByID(a.CoverMediaID.Int64); err == nil && m != nil {
			d.Cover = &mediaJSON{UID: m.UID, URL: m.URL(s.cfg.BaseURL), Mime: m.Mime, Width: m.Width, Height: m.Height, Size: m.Size}
		}
	}
	res := render.Render(a.BodyMD, func(uid string) (string, bool) { return s.media.URLByUID(uid) })
	d.Images = []mediaJSON{}
	for _, uid := range res.Images {
		if m, err := s.mediaByUID(uid); err == nil && m != nil {
			d.Images = append(d.Images, mediaJSON{UID: m.UID, URL: m.URL(s.cfg.BaseURL), Mime: m.Mime, Width: m.Width, Height: m.Height, Size: m.Size})
		}
	}
	return d, nil
}

// ---------- queue / claim ----------

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := atoi(q.Get("limit"), 20)
	if limit < 1 || limit > 100 {
		limit = 20
	}
	platform := strings.ToLower(strings.TrimSpace(q.Get("platform")))
	rows, err := s.db.Query(`SELECT `+jobCols+` FROM publish_jobs j
		WHERE j.status='queued' AND j.active_key=1
		  AND (j.platform=? OR ?='' OR j.platform='')
		ORDER BY j.id LIMIT ?`, platform, platform, limit)
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "查询失败", err.Error())
		return
	}
	defer rows.Close()
	items := []*jobSummary{}
	for _, j := range collect(rows, scanJob) {
		items = append(items, s.jobSummaryOf(j))
	}
	s.writeJSON(w, 200, map[string]interface{}{"items": items})
}

func (s *Server) jobByUIDByID(id int64) (*Job, error) {
	row := s.db.QueryRow("SELECT "+jobCols+" FROM publish_jobs j WHERE j.id = ?"+s.db.lock(), id)
	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return j, err
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	j, err := s.jobByUID(r.PathValue("uid"))
	if err != nil || j == nil {
		s.writeError(w, r, 404, "not_found", "任务不存在", nil)
		return
	}
	d, err := s.jobDetailOf(j)
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "读取任务失败", err.Error())
		return
	}
	s.writeJSON(w, 200, map[string]interface{}{"job": d})
}

func nullStr(v string) interface{} {
	if v == "" {
		return nil
	}
	return v
}

func retryableCode(code string) bool {
	switch code {
	case "network_error", "rate_limited", "captcha_required", "unknown":
		return true
	}
	return false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------- platform review ----------

func (s *Server) handleReviewQueue(w http.ResponseWriter, r *http.Request) {
	counts := map[string]int{}
	rows, err := s.db.Query(`SELECT review_status, COUNT(*) FROM publish_jobs
		WHERE status='succeeded' GROUP BY review_status`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var st string
			var n int
			if rows.Scan(&st, &n) == nil {
				counts[st] = n
			}
		}
	}
	limit := atoi(r.URL.Query().Get("limit"), 20)
	if limit < 1 || limit > 100 {
		limit = 20
	}
	items := []*jobSummary{}
	list, err := s.db.Query(`SELECT `+jobCols+` FROM publish_jobs j
		WHERE j.status='succeeded' AND j.review_status='pending'
		  AND (j.review_next_at IS NULL OR j.review_next_at <= UTC_TIMESTAMP())
		  AND (j.review_lease_expires_at IS NULL OR j.review_lease_expires_at < UTC_TIMESTAMP())
		ORDER BY j.review_next_at IS NULL, j.review_next_at, j.id LIMIT ?`, limit)
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "查询失败", err.Error())
		return
	}
	defer list.Close()
	for _, j := range collect(list, scanJob) {
		items = append(items, s.jobSummaryOf(j))
	}
	s.writeJSON(w, 200, map[string]interface{}{"counts": counts, "items": items})
}

func (s *Server) handleReviewClaim(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Protocol     int    `json:"lease_protocol"`
		WorkerID     string `json:"worker_id"`
		Platform     string `json:"platform"`
		Limit        int    `json:"limit"`
		LeaseSeconds int    `json:"lease_seconds"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, r, 400, "invalid_json", "请求体不是合法 JSON", err.Error())
		return
	}
	if in.Protocol != 2 {
		s.writeError(w, r, 426, "lease_protocol_required", "审核器需升级 lease_protocol=2", nil)
		return
	}
	in.WorkerID = strings.TrimSpace(in.WorkerID)
	if in.WorkerID == "" || len(in.WorkerID) > 100 {
		s.writeError(w, r, 422, "validation_failed", "worker_id 必填，1–100 字", nil)
		return
	}
	if in.Limit <= 0 {
		in.Limit = 1
	}
	if in.Limit > 20 {
		in.Limit = 20
	}
	if in.LeaseSeconds < 120 {
		in.LeaseSeconds = 600
	}
	if in.LeaseSeconds > 3600 {
		in.LeaseSeconds = 3600
	}
	ids := []int64{}
	platform := strings.ToLower(strings.TrimSpace(in.Platform))
	rows, err := s.db.Query(`SELECT j.id FROM publish_jobs j
		WHERE j.status='succeeded' AND j.review_status='pending'
		  AND (j.platform=? OR ?='')
		  AND (j.review_next_at IS NULL OR j.review_next_at <= UTC_TIMESTAMP())
		  AND (j.review_lease_expires_at IS NULL OR j.review_lease_expires_at < UTC_TIMESTAMP())
		ORDER BY j.review_next_at IS NULL, j.review_next_at, j.id LIMIT ?`, platform, platform, in.Limit)
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "查询失败", err.Error())
		return
	}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	items := []*jobSummary{}
	for _, id := range ids {
		token := newLeaseToken()
		cid := clientIDOf(client(r))
		res, err := s.db.Exec(`UPDATE publish_jobs SET review_lease_owner=?,review_lease_token=?,review_client_id=?, review_lease_expires_at=DATE_ADD(UTC_TIMESTAMP(), INTERVAL ? SECOND),
			review_attempts=review_attempts+1, updated_at=UTC_TIMESTAMP()
			WHERE id=? AND status='succeeded' AND review_status='pending'
			  AND (review_next_at IS NULL OR review_next_at <= UTC_TIMESTAMP())
			  AND (review_lease_expires_at IS NULL OR review_lease_expires_at < UTC_TIMESTAMP())`, in.WorkerID, token, cid, in.LeaseSeconds, id)
		if err != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		var uid string
		if err := s.db.QueryRow(`SELECT uid FROM publish_jobs WHERE id=?`, id).Scan(&uid); err != nil {
			continue
		}
		j, err := s.jobByUID(uid)
		if err != nil || j == nil {
			continue
		}
		summary := s.jobSummaryOf(j)
		summary.LeaseToken = token
		items = append(items, summary)
	}
	s.writeJSON(w, 200, map[string]interface{}{"jobs": items, "worker_id": in.WorkerID})
}

func (s *Server) jobArticle(j *Job) (*Article, error) {
	if j.Snapshot.Valid {
		var a Article
		if err := json.Unmarshal([]byte(j.Snapshot.String), &a); err != nil {
			return nil, err
		}
		return &a, nil
	}
	return s.articleByID(j.ArticleID)
}
