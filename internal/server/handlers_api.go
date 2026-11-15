package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"contenthub/internal/media"
	"contenthub/internal/render"
)

var (
	platformRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,49}$`)
	langRe     = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
)

type mediaInput struct {
	UID string `json:"uid"`
	URL string `json:"url"`
}

type mediaJSON struct {
	UID       string `json:"uid"`
	URL       string `json:"url"`
	Mime      string `json:"mime"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Size      int    `json:"size"`
	Duplicate bool   `json:"duplicate,omitempty"`
}

type articleJSON struct {
	Revision         int64      `json:"revision"`
	UID              string     `json:"uid"`
	ExternalID       string     `json:"external_id,omitempty"`
	Platform         string     `json:"platform"`
	PlatformAccount  string     `json:"platform_account,omitempty"`
	Language         string     `json:"language"`
	Topic            string     `json:"topic,omitempty"`
	Tags             []string   `json:"tags"`
	Title            string     `json:"title"`
	Summary          string     `json:"summary"`
	ContentMD        string     `json:"content_md,omitempty"`
	ContentHTML      string     `json:"content_html,omitempty"`
	Cover            *mediaJSON `json:"cover,omitempty"`
	CharCount        int        `json:"char_count"`
	ReadingTime      int        `json:"reading_time"`
	Status           string     `json:"status"`
	PublishStatus    string     `json:"publish_status"`
	PublishMode      string     `json:"publish_mode"`
	ReviewStatus     string     `json:"review_status"`
	ReviewNote       string     `json:"review_note,omitempty"`
	ReviewCheckedAt  *time.Time `json:"review_checked_at,omitempty"`
	ScheduledAt      *time.Time `json:"scheduled_at,omitempty"`
	FirstPublishedAt *time.Time `json:"first_published_at,omitempty"`
	LastPublishedAt  *time.Time `json:"last_published_at,omitempty"`
	JobUID           string     `json:"job_uid,omitempty"`
	ResultAccount    string     `json:"result_account,omitempty"`
	ResultURL        string     `json:"result_url,omitempty"`
	ResultPostID     string     `json:"result_post_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func timePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time.UTC()
	return &v
}

func strOr(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

func (s *Server) articleJSON(a *Article, withBody bool) *articleJSON {
	out := &articleJSON{
		Revision: a.Revision, UID: a.UID, ExternalID: strOr(a.ExternalID), Platform: a.Platform,
		PlatformAccount: strOr(a.PlatformAccount), Language: a.Language, Topic: strOr(a.Topic),
		Tags: a.tags(), Title: a.Title, Summary: a.Summary,
		CharCount: utf8.RuneCountInString(a.BodyText), ReadingTime: a.ReadingTime,
		Status: a.Status, PublishStatus: a.PublishStatus, PublishMode: a.PublishMode,
		ReviewStatus: a.ReviewStatus, ReviewNote: a.ReviewNote, ReviewCheckedAt: timePtr(a.ReviewCheckedAt),
		ScheduledAt: timePtr(a.ScheduledAt), FirstPublishedAt: timePtr(a.FirstPublishedAt),
		LastPublishedAt: timePtr(a.LastPublishedAt),
		CreatedAt:       a.CreatedAt.UTC(), UpdatedAt: a.UpdatedAt.UTC(),
	}
	if withBody {
		out.ContentMD = a.BodyMD
		out.ContentHTML = a.BodyHTML
	}
	if a.CoverMediaID.Valid {
		if m, err := s.mediaByID(a.CoverMediaID.Int64); err == nil && m != nil {
			out.Cover = &mediaJSON{UID: m.UID, URL: m.URL(s.cfg.BaseURL), Mime: m.Mime, Width: m.Width, Height: m.Height, Size: m.Size}
		}
	}
	if j, _ := s.activeJob(a.ID); j != nil {
		out.JobUID = j.UID
	}
	if withBody && a.PublishStatus == "published" {
		if j, _ := s.latestJob(a.ID); j != nil && j.Status == "succeeded" {
			out.ResultAccount = strOr(j.ResultAccount)
			out.ResultURL = strOr(j.ResultURL)
			out.ResultPostID = strOr(j.ResultPostID)
		}
	}
	return out
}

func mediaJSONOf(it *media.Item) *mediaJSON {
	return &mediaJSON{UID: it.UID, URL: it.URL, Mime: it.Mime, Width: it.Width, Height: it.Height, Size: it.Size, Duplicate: it.Duplicate}
}

func (s *Server) resolveMedia(ctx context.Context, in *mediaInput, clientID int64) (int64, error) {
	if in == nil {
		return 0, nil
	}
	if in.UID != "" {
		m, err := s.mediaByUID(in.UID)
		if err != nil || m == nil {
			return 0, errors.New("cover uid not found")
		}
		return m.ID, nil
	}
	if in.URL != "" {
		item, err := s.media.Fetch(ctx, in.URL, clientID)
		if err != nil {
			return 0, fmt.Errorf("fetch cover: %w", err)
		}
		m, err := s.mediaByUID(item.UID)
		if err != nil || m == nil {
			return 0, errors.New("cover not found after fetch")
		}
		return m.ID, nil
	}
	return 0, nil
}

func contentHash(a *Article) string {
	parts := []string{a.Title, a.Summary, a.BodyMD, a.Platform, a.Language, nullableString(a.TagsJSON), fmt.Sprint(a.CoverMediaID.Int64), nullableString(a.PlatformAccount), nullableString(a.Topic)}
	h := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(h[:])
}

func nullableString(v sql.NullString) string {
	if v.Valid {
		return v.String
	}
	return ""
}

// renderInto fills HTML/text/counts from BodyMD.
func (s *Server) renderInto(a *Article) {
	res := render.Render(a.BodyMD, func(uid string) (string, bool) {
		return s.media.URLByUID(uid)
	})
	a.BodyHTML = res.HTML
	a.BodyText = res.Text
	a.WordCount = res.Chars
	a.ReadingTime = res.Minutes
}

func (s *Server) ensureArticleIdle(articleID int64) error {
	j, err := s.activeJob(articleID)
	if err != nil {
		return err
	}
	if j != nil && (j.Status == "claimed" || j.Status == "uncertain") {
		return s.conflict("publication_in_progress", "文章正在发布或结果待核对，暂不可编辑、排期或取消")
	}
	return nil
}

func (s *Server) cancelActiveJob(articleID int64, reason string) error {
	if err := s.ensureArticleIdle(articleID); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE publish_jobs
		SET status='cancelled', active_key=NULL, finished_at=UTC_TIMESTAMP(), cancel_reason=?, updated_at=UTC_TIMESTAMP()
		WHERE article_id=? AND active_key=1 AND status IN ('queued','failed_retryable')`, reason, articleID)
	return err
}

func (s *Server) resetArticleAfterEdit(a *Article) {
	if a.Status == "archived" {
		a.Status = "ready"
	}
	if a.Status != "draft" {
		a.Status = "ready"
	}
	if a.PublishStatus == "scheduled" || a.PublishStatus == "queued" || a.PublishStatus == "publishing" {
		a.PublishStatus = "none"
	}
}

func (s *Server) enqueue(a *Article, at time.Time) (*Job, error) {
	if existing, err := s.activeJob(a.ID); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}
	snapshot, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	uid := requireUID("job_")
	maxAttempts := s.settingInt("publish.max_attempts", 3)
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if maxAttempts > 10 {
		maxAttempts = 10
	}
	var sched interface{}
	if !at.IsZero() {
		sched = at.UTC()
	}
	_, err = s.db.Exec(`INSERT INTO publish_jobs
		(uid, article_id, platform, status, active_key, scheduled_at, next_attempt_at, max_attempts, content_snapshot, created_at, updated_at)
		VALUES (?, ?, ?, 'queued', 1, ?, UTC_TIMESTAMP(), ?, ?, UTC_TIMESTAMP(), UTC_TIMESTAMP())`,
		uid, a.ID, a.Platform, sched, maxAttempts, string(snapshot))
	if err != nil {
		return nil, err
	}
	if _, err = s.db.Exec("UPDATE articles SET publish_status='queued',revision=revision+1, updated_at=UTC_TIMESTAMP() WHERE id=?", a.ID); err != nil {
		return nil, err
	}
	a.Revision++
	a.PublishStatus = "queued"
	return s.jobByUID(uid)
}

func (s *Server) scheduleArticle(a *Article, at time.Time) error {
	if at.Before(time.Now()) {
		at = time.Now()
	}
	_ = s.cancelActiveJob(a.ID, "rescheduled")
	a.ScheduledAt = sql.NullTime{Time: at.UTC(), Valid: true}
	a.Status = "ready"
	if err := s.updateArticle(a); err != nil {
		return err
	}
	if a.PublishMode == "manual" {
		if a.PublishStatus != "published" {
			a.PublishStatus = "none"
			_, err := s.db.Exec("UPDATE articles SET publish_status='none', updated_at=UTC_TIMESTAMP() WHERE id=?", a.ID)
			return err
		}
		return nil
	}
	if !at.After(time.Now().Add(5 * time.Second)) {
		if _, err := s.enqueue(a, at); err != nil {
			return err
		}
	} else {
		_, err := s.db.Exec("UPDATE articles SET publish_status='scheduled', updated_at=UTC_TIMESTAMP() WHERE id=?", a.ID)
		a.PublishStatus = "scheduled"
		return err
	}
	return nil
}

func (s *Server) cancelArticle(a *Article, reason string) error {
	if err := s.cancelActiveJob(a.ID, reason); err != nil {
		return err
	}
	a.ScheduledAt = sql.NullTime{}
	a.PublishStatus = "cancelled"
	if err := s.updateArticle(a); err != nil {
		return err
	}
	return nil
}

// ---------- request shapes ----------

type articleCreate struct {
	Revision        *int64      `json:"revision"`
	ExternalID      *string     `json:"external_id"`
	Platform        string      `json:"platform"`
	PlatformAccount *string     `json:"platform_account"`
	Language        string      `json:"language"`
	Topic           *string     `json:"topic"`
	Tags            []string    `json:"tags"`
	Title           string      `json:"title"`
	Summary         string      `json:"summary"`
	BodyMD          string      `json:"body_md"`
	Cover           *mediaInput `json:"cover"`
	PublishMode     string      `json:"publish_mode"`
	Status          string      `json:"status"`
	ScheduledAt     *string     `json:"scheduled_at"`
}

type articlePatch struct {
	Revision        *int64          `json:"revision"`
	ExternalID      *string         `json:"external_id"`
	Platform        *string         `json:"platform"`
	PlatformAccount *string         `json:"platform_account"`
	Language        *string         `json:"language"`
	Topic           *string         `json:"topic"`
	Tags            *[]string       `json:"tags"`
	Title           *string         `json:"title"`
	Summary         *string         `json:"summary"`
	BodyMD          *string         `json:"body_md"`
	Cover           json.RawMessage `json:"cover"`
	PublishMode     *string         `json:"publish_mode"`
	Status          *string         `json:"status"`
	ScheduledAt     *string         `json:"scheduled_at"`
	ClearSchedule   bool            `json:"clear_schedule"`
}

func validateCommon(platform, language, title, summary string, tags []string) map[string]string {
	errs := map[string]string{}
	if platform != "" && !platformRe.MatchString(platform) {
		errs["platform"] = "平台标识需为小写字母、数字、下划线或短横线，1–50 位"
	}
	if language == "" || len(language) > 16 || !langRe.MatchString(language) {
		errs["language"] = "language 需为 BCP-47 形式，例如 zh-CN、en"
	}
	if strings.TrimSpace(title) == "" {
		errs["title"] = "标题不能为空"
	}
	if utf8.RuneCountInString(title) > 200 {
		errs["title"] = "标题最多 200 字"
	}
	if utf8.RuneCountInString(summary) > 500 {
		errs["summary"] = "摘要最多 500 字"
	}
	if len(tags) > 20 {
		errs["tags"] = "标签最多 20 个"
	}
	for _, t := range tags {
		if strings.TrimSpace(t) == "" || utf8.RuneCountInString(t) > 32 {
			errs["tags"] = "每个标签 1–32 字"
			break
		}
	}
	return errs
}

func normalizePlatform(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

// ---------- public API ----------

func (s *Server) handleCreateArticle(w http.ResponseWriter, r *http.Request) {
	var in articleCreate
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "invalid_json", "请求体不是合法 JSON", err.Error())
		return
	}
	c := client(r)
	// Platform is decided by the publisher and written back on the result receipt.
	in.Platform = ""
	if in.Language == "" {
		in.Language = "zh-CN"
	}
	if in.Status != "" && in.Status != "draft" && in.Status != "ready" {
		s.writeError(w, r, 422, "validation_failed", "status 只能是 draft/ready", nil)
		return
	}
	if in.Status == "" {
		in.Status = "ready"
	}
	if in.PublishMode == "" {
		in.PublishMode = "agent"
	}
	if in.PublishMode != "agent" && in.PublishMode != "manual" {
		s.writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "publish_mode 只能是 agent 或 manual", nil)
		return
	}
	if errs := validateCommon(in.Platform, in.Language, in.Title, in.Summary, in.Tags); len(errs) > 0 {
		s.writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "参数校验失败", errs)
		return
	}
	if strings.TrimSpace(in.BodyMD) == "" {
		s.writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "body_md 不能为空", map[string]string{"body_md": "正文不能为空"})
		return
	}

	var scheduled *time.Time
	if in.ScheduledAt != nil && *in.ScheduledAt != "" {
		t, err := time.Parse(time.RFC3339, *in.ScheduledAt)
		if err != nil {
			s.writeError(w, r, 422, "validation_failed", "scheduled_at 需为 RFC3339", nil)
			return
		}
		t = t.UTC()
		scheduled = &t
	}

	if in.ExternalID != nil && *in.ExternalID != "" {
		var existing *Article
		if c != nil {
			row := s.db.QueryRow("SELECT "+articleCols+" FROM articles a WHERE a.client_id=? AND a.external_id=? AND a.deleted_at IS NULL"+s.db.lock(), c.ID, *in.ExternalID)
			existing, _ = scanArticle(row)
		}
		if existing != nil {
			if !s.checkRevision(w, r, in.Revision, existing) {
				return
			}
			updated, job, err := s.updateFromInput(r.Context(), existing, in, scheduled, c)
			if err != nil {
				if err.Error() == "platform_locked" {
					s.writeError(w, r, 409, "platform_locked", "队列中或已发布的文章不能修改平台", nil)
					return
				}
				s.writeError(w, r, 500, "internal_error", "更新失败", err.Error())
				return
			}
			out := s.articleJSON(updated, true)
			if job != nil {
				out.JobUID = job.UID
			}
			s.writeJSON(w, http.StatusOK, map[string]interface{}{"article": out, "created": false})
			return
		}
	}

	a := &Article{
		UID: requireUID("art_"), Platform: in.Platform, Language: in.Language,
		Title: in.Title, Summary: in.Summary, BodyMD: in.BodyMD, PublishMode: in.PublishMode,
		Status: "ready", PublishStatus: "none",
	}
	if c != nil {
		a.ClientID = sql.NullInt64{Int64: c.ID, Valid: true}
	}
	if in.ExternalID != nil && *in.ExternalID != "" {
		a.ExternalID = sql.NullString{String: *in.ExternalID, Valid: true}
	}
	if in.PlatformAccount != nil {
		a.PlatformAccount = sql.NullString{String: truncate(*in.PlatformAccount, 100), Valid: true}
	}
	if in.Topic != nil {
		a.Topic = sql.NullString{String: *in.Topic, Valid: true}
	}
	if len(in.Tags) > 0 {
		raw, _ := json.Marshal(in.Tags)
		a.TagsJSON = sql.NullString{String: string(raw), Valid: true}
	}
	if scheduled != nil {
		a.ScheduledAt = sql.NullTime{Time: *scheduled, Valid: true}
	}
	if in.Status == "draft" {
		a.Status = "draft"
	}
	coverID, err := s.resolveMedia(r.Context(), in.Cover, clientIDOf(c))
	if err != nil {
		s.writeError(w, r, 422, "media_invalid", "封面处理失败", err.Error())
		return
	}
	if coverID > 0 {
		a.CoverMediaID = sql.NullInt64{Int64: coverID, Valid: true}
	}
	if strings.TrimSpace(a.BodyMD) == "" {
		s.writeError(w, r, 422, "validation_failed", "正文不能为空", nil)
		return
	}
	s.renderInto(a)
	a.ContentHash = sql.NullString{String: contentHash(a), Valid: true}
	if err := s.insertArticle(a); err != nil {
		s.writeError(w, r, 500, "internal_error", "创建失败", err.Error())
		return
	}
	if fresh, err := s.articleByID(a.ID); err == nil && fresh != nil {
		a = fresh
	}
	var job *Job
	if a.Status == "ready" && a.PublishMode == "agent" {
		if scheduled != nil && scheduled.After(time.Now()) {
			a.PublishStatus = "scheduled"
			_ = s.updateArticle(a)
		} else if a.PublishMode == "agent" {
			job, _ = s.enqueue(a, time.Time{})
		}
	}
	if fresh, err := s.articleByID(a.ID); err == nil && fresh != nil {
		a = fresh
	}
	out := s.articleJSON(a, true)
	if job != nil {
		out.JobUID = job.UID
	}
	s.writeJSON(w, http.StatusCreated, map[string]interface{}{"article": out, "created": true})
}

func canAccessArticle(r *http.Request, a *Article) bool {
	c := client(r)
	if c == nil || c.Type == "admin" {
		return true
	}
	return a.ClientID.Valid && a.ClientID.Int64 == c.ID
}

func clientIDOf(c *apiClient) int64 {
	if c == nil {
		return 0
	}
	return c.ID
}

func truncate(v string, n int) string {
	r := []rune(strings.TrimSpace(v))
	if len(r) > n {
		return string(r[:n])
	}
	return string(r)
}

func (s *Server) updateFromInput(ctx context.Context, a *Article, in articleCreate, scheduled *time.Time, c *apiClient) (*Article, *Job, error) {
	coverID := a.CoverMediaID.Int64
	if in.Cover != nil {
		id, err := s.resolveMedia(ctx, in.Cover, clientIDOf(c))
		if err != nil {
			return nil, nil, err
		}
		coverID = id
	}
	account := a.PlatformAccount
	if in.PlatformAccount != nil {
		account = sql.NullString{String: truncate(*in.PlatformAccount, 100), Valid: true}
	}
	topic := a.Topic
	if in.Topic != nil {
		topic = sql.NullString{String: *in.Topic, Valid: true}
	}
	if a.Status == "archived" {
		return nil, nil, s.conflict("archived", "已归档文章需先恢复")
	}
	if err := s.ensureArticleIdle(a.ID); err != nil {
		return nil, nil, err
	}
	_ = s.cancelActiveJob(a.ID, "content_updated")
	a.Language = in.Language
	a.Title = in.Title
	a.Summary = in.Summary
	a.BodyMD = in.BodyMD
	a.PublishMode = in.PublishMode
	a.PlatformAccount = account
	a.Topic = topic
	a.TagsJSON = tagsJSON(in.Tags)
	if coverID > 0 {
		a.CoverMediaID = sql.NullInt64{Int64: coverID, Valid: true}
	}
	s.renderInto(a)
	a.ContentHash = sql.NullString{String: contentHash(a), Valid: true}
	if a.PublishStatus != "published" {
		a.PublishStatus = "none"
	}
	a.Status = in.Status
	if a.Status == "draft" {
		a.ScheduledAt = sql.NullTime{}
	} else if scheduled != nil {
		a.ScheduledAt = sql.NullTime{Time: *scheduled, Valid: true}
	}
	if err := s.updateArticle(a); err != nil {
		return nil, nil, err
	}
	var job *Job
	if a.Status == "ready" && a.PublishMode == "agent" && a.PublishStatus != "published" && a.ScheduledAt.Valid && a.ScheduledAt.Time.After(time.Now()) {
		a.PublishStatus = "scheduled"
		_ = s.updateArticle(a)
	} else if a.Status == "ready" && a.PublishStatus != "published" && a.PublishMode == "agent" {
		job, _ = s.enqueue(a, time.Time{})
	}
	return a, job, nil
}

func tagsJSON(tags []string) sql.NullString {
	if len(tags) == 0 {
		return sql.NullString{}
	}
	raw, _ := json.Marshal(tags)
	return sql.NullString{String: string(raw), Valid: true}
}

func (s *Server) handleGetArticle(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	a, err := s.articleByUID(uid)
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "查询失败", err.Error())
		return
	}
	if a == nil {
		s.writeError(w, r, 404, "not_found", "文章不存在", nil)
		return
	}
	if c := client(r); c != nil && c.Type != "admin" && !canAccessArticle(r, a) {
		s.writeError(w, r, 404, "not_found", "文章不存在", nil)
		return
	}
	s.writeJSON(w, 200, map[string]interface{}{"article": s.articleJSON(a, true)})
}

func (s *Server) handlePatchArticle(w http.ResponseWriter, r *http.Request) {
	a, err := s.articleByUID(r.PathValue("uid"))
	if err != nil || a == nil {
		s.writeError(w, r, 404, "not_found", "文章不存在", nil)
		return
	}
	if c := client(r); c != nil && c.Type != "admin" && (!a.ClientID.Valid || a.ClientID.Int64 != c.ID) {
		s.writeError(w, r, 404, "not_found", "文章不存在", nil)
		return
	}
	var in articlePatch
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, r, 400, "invalid_json", "请求体不是合法 JSON", err.Error())
		return
	}
	if !s.checkRevision(w, r, in.Revision, a) {
		return
	}
	status := a.Status
	if in.Status != nil {
		status = *in.Status
	}
	if status != "draft" && status != "ready" && status != "archived" {
		s.writeError(w, r, 422, "validation_failed", "status 只能是 draft/ready/archived", nil)
		return
	}
	restoring := a.Status == "archived" && in.Status != nil && (*in.Status == "draft" || *in.Status == "ready")
	if a.Status == "archived" && !restoring {
		s.writeError(w, r, 409, "archived", "已归档文章不可编辑，请先恢复为草稿或待发布", nil)
		return
	}
	if in.PlatformAccount != nil {
		a.PlatformAccount = sql.NullString{String: truncate(*in.PlatformAccount, 100), Valid: true}
	}
	if in.Language != nil {
		if !langRe.MatchString(*in.Language) {
			s.writeError(w, r, 422, "validation_failed", "language 不合法", nil)
			return
		}
		a.Language = *in.Language
	}
	if in.Topic != nil {
		a.Topic = sql.NullString{String: *in.Topic, Valid: true}
	}
	if in.Tags != nil {
		a.TagsJSON = tagsJSON(*in.Tags)
	}
	if in.Title != nil {
		a.Title = *in.Title
	}
	if in.Summary != nil {
		a.Summary = *in.Summary
	}
	if in.BodyMD != nil {
		a.BodyMD = *in.BodyMD
	}
	if in.PublishMode != nil {
		if *in.PublishMode != "agent" && *in.PublishMode != "manual" {
			s.writeError(w, r, 422, "validation_failed", "publish_mode 不合法", nil)
			return
		}
		a.PublishMode = *in.PublishMode
	}
	if in.Cover != nil {
		if string(in.Cover) == "null" {
			a.CoverMediaID = sql.NullInt64{}
		} else {
			var cover mediaInput
			if err := json.Unmarshal(in.Cover, &cover); err != nil {
				s.writeError(w, r, 422, "media_invalid", "cover 不合法", nil)
				return
			}
			id, err := s.resolveMedia(r.Context(), &cover, clientIDOf(client(r)))
			if err != nil {
				s.writeError(w, r, 422, "media_invalid", "封面处理失败", err.Error())
				return
			}
			a.CoverMediaID = sql.NullInt64{Int64: id, Valid: id > 0}
		}
	}
	if in.ClearSchedule {
		a.ScheduledAt = sql.NullTime{}
	} else if in.ScheduledAt != nil && *in.ScheduledAt != "" {
		t, err := time.Parse(time.RFC3339, *in.ScheduledAt)
		if err != nil {
			s.writeError(w, r, 422, "validation_failed", "scheduled_at 需为 RFC3339", nil)
			return
		}
		a.ScheduledAt = sql.NullTime{Time: t.UTC(), Valid: true}
	}
	if errs := validateCommon(a.Platform, a.Language, a.Title, a.Summary, a.tags()); len(errs) > 0 {
		s.writeError(w, r, 422, "validation_failed", "参数校验失败", errs)
		return
	}
	if status == "archived" {
		_ = s.cancelActiveJob(a.ID, "archived")
		a.Status = "archived"
		a.ScheduledAt = sql.NullTime{}
		if a.PublishStatus != "published" {
			a.PublishStatus = "none"
		}
	} else if status == "draft" {
		_ = s.cancelActiveJob(a.ID, "draft")
		a.Status = "draft"
		a.ScheduledAt = sql.NullTime{}
		if a.PublishStatus != "published" {
			a.PublishStatus = "none"
		}
	} else {
		a.Status = "ready"
	}
	if strings.TrimSpace(a.BodyMD) == "" {
		s.writeError(w, r, 422, "validation_failed", "正文不能为空", nil)
		return
	}
	s.renderInto(a)
	a.ContentHash = sql.NullString{String: contentHash(a), Valid: true}
	if err := s.updateArticle(a); err != nil {
		s.writeError(w, r, 500, "internal_error", "更新失败", err.Error())
		return
	}
	var job *Job
	if a.Status == "ready" && a.PublishMode == "agent" {
		if a.ScheduledAt.Valid && a.ScheduledAt.Time.After(time.Now()) && a.PublishStatus != "published" {
			_ = s.cancelActiveJob(a.ID, "rescheduled")
			a.PublishStatus = "scheduled"
			_ = s.updateArticle(a)
		} else if a.PublishStatus != "published" {
			_ = s.cancelActiveJob(a.ID, "content_updated")
			job, _ = s.enqueue(a, time.Time{})
		}
	} else if a.Status == "ready" && a.PublishMode == "manual" && a.PublishStatus != "published" {
		_ = s.cancelActiveJob(a.ID, "manual_mode")
		a.PublishStatus = "none"
		_ = s.updateArticle(a)
	}
	out := s.articleJSON(a, true)
	if job != nil {
		out.JobUID = job.UID
	}
	s.writeJSON(w, 200, map[string]interface{}{"article": out})
}

func (s *Server) handleListArticles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pageMode := q.Get("page") != ""
	limit := atoi(q.Get("limit"), 20)
	if limit < 1 || limit > 100 {
		limit = 20
	}
	page, size := atoi(q.Get("page"), 1), atoi(q.Get("page_size"), 20)
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	where := []string{"a.deleted_at IS NULL"}
	args := []any{}
	if c := client(r); c != nil && c.Type != "admin" {
		where = append(where, "a.client_id = ?")
		args = append(args, c.ID)
	}
	if v := q.Get("status"); v != "" {
		where = append(where, "a.status = ?")
		args = append(args, v)
	}
	if v := q.Get("publish_status"); v != "" {
		where = append(where, "a.publish_status = ?")
		args = append(args, v)
	}
	if v := q.Get("review_status"); v != "" {
		where = append(where, "a.review_status = ?")
		args = append(args, v)
	}
	if v := q.Get("platform"); v != "" {
		if v == "__none__" {
			where = append(where, "a.platform = ''")
		} else {
			where = append(where, "a.platform = ?")
			args = append(args, v)
		}
	}
	if v := q.Get("language"); v != "" {
		where = append(where, "a.language = ?")
		args = append(args, v)
	}
	if v := q.Get("q"); v != "" {
		where = append(where, "(a.title LIKE ? OR a.summary LIKE ?)")
		args = append(args, "%"+v+"%", "%"+v+"%")
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			where = append(where, "a.created_at >= ?")
			args = append(args, t.UTC())
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			where = append(where, "a.created_at <= ?")
			args = append(args, t.UTC())
		}
	}
	if !pageMode {
		if v := q.Get("cursor"); v != "" {
			where = append(where, "a.id < ?")
			args = append(args, atoi(v, 1<<62))
		}
	}
	whereSQL := strings.Join(where, " AND ")
	if pageMode {
		var total int
		if err := s.db.QueryRow("SELECT COUNT(*) FROM articles a WHERE "+whereSQL, args...).Scan(&total); err != nil {
			s.writeError(w, r, 500, "internal_error", "统计失败", err.Error())
			return
		}
		qargs := append(append([]any{}, args...), size, (page-1)*size)
		rows, err := s.db.Query("SELECT "+articleCols+" FROM articles a WHERE "+whereSQL+" ORDER BY a.id DESC LIMIT ? OFFSET ?", qargs...)
		if err != nil {
			s.writeError(w, r, 500, "internal_error", "查询失败", err.Error())
			return
		}
		defer rows.Close()
		items := []*articleJSON{}
		for _, a := range collect(rows, scanArticle) {
			items = append(items, s.articleJSON(a, false))
		}
		s.writeJSON(w, 200, map[string]interface{}{"items": items, "total": total, "page": page, "page_size": size})
		return
	}
	args = append(args, limit+1)
	rows, err := s.db.Query("SELECT "+articleCols+" FROM articles a WHERE "+whereSQL+" ORDER BY a.id DESC LIMIT ?", args...)
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "查询失败", err.Error())
		return
	}
	defer rows.Close()
	items := []*articleJSON{}
	var lastVisibleID int64
	for _, a := range collect(rows, scanArticle) {
		if len(items) < limit {
			lastVisibleID = a.ID
		}
		items = append(items, s.articleJSON(a, false))
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = fmt.Sprint(lastVisibleID)
	}
	resp := map[string]interface{}{"items": items, "next_cursor": next}
	s.writeJSON(w, 200, resp)
}

func (s *Server) handleArticleStats(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	where := []string{"a.deleted_at IS NULL"}
	args := []any{}
	if c := client(r); c != nil && c.Type != "admin" {
		where = append(where, "a.client_id = ?")
		args = append(args, c.ID)
	}
	if v := q.Get("q"); v != "" {
		where = append(where, "(a.title LIKE ? OR a.summary LIKE ?)")
		args = append(args, "%"+v+"%", "%"+v+"%")
	}
	if v := q.Get("language"); v != "" {
		where = append(where, "a.language = ?")
		args = append(args, v)
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			where = append(where, "a.created_at >= ?")
			args = append(args, t.UTC())
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			where = append(where, "a.created_at <= ?")
			args = append(args, t.UTC())
		}
	}
	whereSQL := strings.Join(where, " AND ")
	var total, draft, ready, archived int
	var pubNone, scheduled, queued, publishing, published, failed, cancelled, uncertain int
	var revNone, revPending, revApproved, revRejected int
	err := s.db.QueryRow(`SELECT COUNT(*),
		COUNT(CASE WHEN a.status='draft' THEN 1 END),
		COUNT(CASE WHEN a.status='ready' THEN 1 END),
		COUNT(CASE WHEN a.status='archived' THEN 1 END),
		COUNT(CASE WHEN a.publish_status='none' THEN 1 END),
		COUNT(CASE WHEN a.publish_status='scheduled' THEN 1 END),
		COUNT(CASE WHEN a.publish_status='queued' THEN 1 END),
		COUNT(CASE WHEN a.publish_status='publishing' THEN 1 END),
		COUNT(CASE WHEN a.publish_status='published' THEN 1 END),
		COUNT(CASE WHEN a.publish_status='failed' THEN 1 END),
		COUNT(CASE WHEN a.publish_status='cancelled' THEN 1 END),
 COUNT(CASE WHEN a.publish_status='uncertain' THEN 1 END),
		COUNT(CASE WHEN a.review_status='none' THEN 1 END),
		COUNT(CASE WHEN a.review_status='pending' THEN 1 END),
		COUNT(CASE WHEN a.review_status='approved' THEN 1 END),
		COUNT(CASE WHEN a.review_status='rejected' THEN 1 END)
		FROM articles a WHERE `+whereSQL, args...).
		Scan(&total, &draft, &ready, &archived, &pubNone, &scheduled, &queued, &publishing, &published, &failed, &cancelled, &uncertain, &revNone, &revPending, &revApproved, &revRejected)
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "统计失败", err.Error())
		return
	}
	type platformCount struct {
		Platform string `json:"platform"`
		Count    int    `json:"count"`
	}
	platforms := []platformCount{}
	rows, err := s.db.Query("SELECT a.platform, COUNT(*) FROM articles a WHERE "+whereSQL+" GROUP BY a.platform ORDER BY COUNT(*) DESC, a.platform ASC", args...)
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "统计失败", err.Error())
		return
	}
	defer rows.Close()
	for rows.Next() {
		var p platformCount
		if err := rows.Scan(&p.Platform, &p.Count); err != nil {
			s.writeError(w, r, 500, "internal_error", "统计失败", err.Error())
			return
		}
		platforms = append(platforms, p)
	}
	s.writeJSON(w, 200, map[string]interface{}{
		"total":  total,
		"status": map[string]int{"draft": draft, "ready": ready, "archived": archived},
		"publish_status": map[string]int{
			"none": pubNone, "scheduled": scheduled, "queued": queued,
			"publishing": publishing, "published": published, "failed": failed, "cancelled": cancelled, "uncertain": uncertain,
		},
		"review_status": map[string]int{
			"none": revNone, "pending": revPending, "approved": revApproved, "rejected": revRejected,
		},
		"platforms": platforms,
	})
}

func (s *Server) handleSchedule(w http.ResponseWriter, r *http.Request) {
	a, err := s.articleByUID(r.PathValue("uid"))
	if err != nil || a == nil {
		s.writeError(w, r, 404, "not_found", "文章不存在", nil)
		return
	}
	if !canAccessArticle(r, a) {
		s.writeError(w, r, 404, "not_found", "文章不存在", nil)
		return
	}
	var in struct {
		ScheduledAt *string `json:"scheduled_at"`
		Account     string  `json:"platform_account"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, r, 400, "invalid_json", "请求体不是合法 JSON", err.Error())
		return
	}
	if in.Account != "" {
		a.PlatformAccount = sql.NullString{String: truncate(in.Account, 100), Valid: true}
	}
	if a.Status != "ready" {
		s.writeError(w, r, 409, "invalid_state", "只有 ready 状态可排期", map[string]string{"status": a.Status})
		return
	}
	if in.ScheduledAt == nil || strings.TrimSpace(*in.ScheduledAt) == "" {
		if j, _ := s.activeJob(a.ID); j != nil {
			s.writeError(w, r, 409, "invalid_state", "已有活跃的发布任务，不能取消排期", map[string]string{"job": j.UID})
			return
		}
		a.ScheduledAt = sql.NullTime{}
		if a.PublishStatus == "scheduled" {
			a.PublishStatus = "none"
		}
		if a.PublishMode == "agent" && a.PublishStatus != "published" {
			if _, err := s.enqueue(a, time.Time{}); err != nil {
				s.writeError(w, r, 500, "internal_error", "入队失败", err.Error())
				return
			}
		}
		if err := s.updateArticle(a); err != nil {
			s.writeError(w, r, 500, "internal_error", "更新失败", err.Error())
			return
		}
		updated, _ := s.articleByUID(a.UID)
		s.writeJSON(w, 200, map[string]interface{}{"article": s.articleJSON(updated, true)})
		return
	}
	at, err := time.Parse(time.RFC3339, *in.ScheduledAt)
	if err != nil {
		s.writeError(w, r, 422, "validation_failed", "scheduled_at 需为 RFC3339 或 null", nil)
		return
	}
	if err := s.scheduleArticle(a, at); err != nil {
		s.writeError(w, r, 500, "internal_error", "排期失败", err.Error())
		return
	}
	updated, _ := s.articleByUID(a.UID)
	s.writeJSON(w, 200, map[string]interface{}{"article": s.articleJSON(updated, true)})
}

func (s *Server) handleCancelArticle(w http.ResponseWriter, r *http.Request) {
	a, err := s.articleByUID(r.PathValue("uid"))
	if err != nil || a == nil {
		s.writeError(w, r, 404, "not_found", "文章不存在", nil)
		return
	}
	if !canAccessArticle(r, a) {
		s.writeError(w, r, 404, "not_found", "文章不存在", nil)
		return
	}
	if a.PublishStatus != "scheduled" && a.PublishStatus != "queued" && a.PublishStatus != "publishing" {
		s.writeError(w, r, 409, "invalid_state", "当前没有可取消的发布", map[string]string{"publish_status": a.PublishStatus})
		return
	}
	if err := s.cancelArticle(a, "cancelled_by_user"); err != nil {
		s.writeError(w, r, 500, "internal_error", "取消失败", err.Error())
		return
	}
	s.writeJSON(w, 200, map[string]interface{}{"article": s.articleJSON(a, true)})
}

// ---------- media ----------

func (s *Server) handleUploadMedia(w http.ResponseWriter, r *http.Request) {
	maxMB := s.settingInt("media.max_upload_mb", 10)
	if maxMB < 1 {
		maxMB = 10
	}
	data, err := readUpload(r, int64(maxMB)<<20)
	if err != nil {
		s.writeError(w, r, 413, "too_large", "图片过大或读取失败", err.Error())
		return
	}
	item, err := s.media.SaveBytesContext(r.Context(), data, "upload", clientIDOf(client(r)))
	if err != nil {
		s.writeError(w, r, 422, "media_invalid", "不是有效图片", err.Error())
		return
	}
	s.writeJSON(w, 201, map[string]interface{}{"media": mediaJSONOf(item)})
}

func (s *Server) handleMediaFromURL(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL string `json:"url"`
	}
	if err := decodeJSON(r, &in); err != nil || in.URL == "" {
		s.writeError(w, r, 400, "invalid_json", "需要 url", nil)
		return
	}
	item, err := s.media.Fetch(r.Context(), in.URL, clientIDOf(client(r)))
	if err != nil {
		s.writeError(w, r, 422, "media_invalid", "抓取图片失败", err.Error())
		return
	}
	s.writeJSON(w, 201, map[string]interface{}{"media": mediaJSONOf(item)})
}

func readUpload(r *http.Request, max int64) ([]byte, error) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		r.Body = http.MaxBytesReader(nil, r.Body, max+(1<<20))
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			return nil, err
		}
		defer r.MultipartForm.RemoveAll()
		f, _, err := r.FormFile("file")
		if err != nil {
			return nil, err
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, max+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > max {
			return nil, errors.New("file too large")
		}
		return data, nil
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, errors.New("file too large")
	}
	return data, nil
}

func (s *Server) checkRevision(w http.ResponseWriter, r *http.Request, revision *int64, a *Article) bool {
	if revision == nil {
		s.writeError(w, r, 428, "revision_required", "请携带当前文章 revision", nil)
		return false
	}
	if *revision != a.Revision {
		s.writeError(w, r, 409, "revision_conflict", "文章已更新，请重新载入后保存", map[string]int64{"revision": a.Revision})
		return false
	}
	return true
}
