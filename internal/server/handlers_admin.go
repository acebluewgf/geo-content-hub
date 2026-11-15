package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"contenthub/internal/release"
	"contenthub/internal/render"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	dbOK := true
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		dbOK = false
	}
	status := "ok"
	schedulerOK := dbOK && s.schedulerHealthy()
	if !dbOK || !schedulerOK {
		status = "degraded"
	}
	code := 200
	if status != "ok" {
		code = 503
	}
	s.writeJSON(w, code, map[string]interface{}{
		"status":    status,
		"db":        dbOK,
		"scheduler": schedulerOK,
		"version":   release.Version(),
		"time":      time.Now().UTC(),
	})
}

// ---------- auth ----------

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, r, 400, "invalid_json", "请求体不合法", nil)
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	var (
		id          int64
		uid, uname  string
		hash, disp  string
		role        string
		failCount   int
		lockedUntil sql.NullTime
	)
	err := s.db.QueryRow(`SELECT id, uid, username, password_hash, display_name, role, login_fail_count, locked_until
		FROM users WHERE username=? AND status=1`, in.Username).
		Scan(&id, &uid, &uname, &hash, &disp, &role, &failCount, &lockedUntil)
	if err != nil {
		s.writeError(w, r, 401, "invalid_credentials", "用户名或密码错误", nil)
		return
	}
	if lockedUntil.Valid && lockedUntil.Time.After(time.Now().UTC()) {
		s.writeError(w, r, 429, "account_locked", "登录失败次数过多，请 15 分钟后再试", nil)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil {
		_, _ = s.db.Exec(`UPDATE users SET locked_until=IF(login_fail_count+1>=5,DATE_ADD(UTC_TIMESTAMP(),INTERVAL 15 MINUTE),locked_until),login_fail_count=IF(login_fail_count+1>=5,0,login_fail_count+1) WHERE id=?`, id)
		s.writeError(w, r, 401, "invalid_credentials", "用户名或密码错误", nil)
		return
	}
	_, _ = s.db.Exec("UPDATE users SET login_fail_count=0, locked_until=NULL, last_login_at=UTC_TIMESTAMP() WHERE id=?", id)
	u := &sessionUser{UID: uid, Username: uname, Display: disp, Role: role}
	if err := s.setSession(w, r, u, hash); err != nil {
		s.writeError(w, r, 503, "session_unavailable", "登录暂不可用", nil)
		return
	}
	s.writeJSON(w, 200, map[string]interface{}{"user": u, "csrf_token": s.ensureCSRF(w, r)})
}

func (s *Server) ensureCSRF(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie("ch_csrf"); err == nil && c.Value != "" {
		return c.Value
	}
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	v := base64.RawURLEncoding.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{Name: "ch_csrf", Value: v, Path: "/", Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600})
	return v
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("ch_session"); err == nil {
		if _, err = s.db.Exec("DELETE FROM user_sessions WHERE token_hash=?", tokenDigest(c.Value)); err != nil {
			s.writeError(w, r, 503, "logout_failed", "退出失败，请重试", nil)
			return
		}
	}
	s.clearSession(w)
	s.writeJSON(w, 200, map[string]interface{}{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, 200, map[string]interface{}{"user": user(r), "csrf_token": s.ensureCSRF(w, r)})
}

func (s *Server) handleCSRF(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, 200, map[string]interface{}{"csrf_token": s.ensureCSRF(w, r)})
}

// ---------- dashboard ----------

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	counts := map[string]int{}
	rows, err := s.db.Query("SELECT status, COUNT(*) FROM articles WHERE deleted_at IS NULL GROUP BY status")
	if err == nil {
		for rows.Next() {
			var k string
			var n int
			if rows.Scan(&k, &n) == nil {
				counts["status:"+k] = n
			}
		}
		rows.Close()
	}
	rows, err = s.db.Query("SELECT publish_status, COUNT(*) FROM articles WHERE deleted_at IS NULL GROUP BY publish_status")
	if err == nil {
		for rows.Next() {
			var k string
			var n int
			if rows.Scan(&k, &n) == nil {
				counts["publish:"+k] = n
			}
		}
		rows.Close()
	}
	var queueQueued, queueClaimed, queueFailed, todayPublished int
	_ = s.db.QueryRow("SELECT COUNT(*) FROM publish_jobs WHERE status='queued'").Scan(&queueQueued)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM publish_jobs WHERE status='claimed'").Scan(&queueClaimed)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM publish_jobs WHERE status='failed_permanent'").Scan(&queueFailed)
	_ = s.db.QueryRow("SELECT COUNT(*) FROM articles WHERE publish_status='published' AND last_published_at >= UTC_DATE()").Scan(&todayPublished)

	recent := []*articleJSON{}
	rows, err = s.db.Query("SELECT " + articleCols + " FROM articles a WHERE a.deleted_at IS NULL ORDER BY a.id DESC LIMIT 8")
	if err == nil {
		for _, a := range collect(rows, scanArticle) {
			recent = append(recent, s.articleJSON(a, false))
		}
		rows.Close()
	}
	s.writeJSON(w, 200, map[string]interface{}{
		"counts": counts,
		"queue": map[string]int{
			"queued": queueQueued, "claimed": queueClaimed, "failed": queueFailed,
		},
		"today_published": todayPublished,
		"recent":          recent,
		"scheduler":       s.schedulerHealthy(),
	})
}

// ---------- admin articles ----------

func (s *Server) handleAdminArticles(w http.ResponseWriter, r *http.Request) {
	s.handleListArticles(w, r)
}

func (s *Server) handleAdminCreateArticle(w http.ResponseWriter, r *http.Request) {
	s.handleCreateArticle(w, r)
}

func (s *Server) handleAdminArticle(w http.ResponseWriter, r *http.Request) {
	s.handleGetArticle(w, r)
}

func (s *Server) handleAdminPatchArticle(w http.ResponseWriter, r *http.Request) {
	s.handlePatchArticle(w, r)
}

func (s *Server) handleArchive(w http.ResponseWriter, r *http.Request) {
	a, err := s.articleByUID(r.PathValue("uid"))
	if err != nil || a == nil {
		s.writeError(w, r, 404, "not_found", "文章不存在", nil)
		return
	}
	_ = s.cancelActiveJob(a.ID, "archived")
	a.Status = "archived"
	a.ScheduledAt = sql.NullTime{}
	if a.PublishStatus != "published" {
		a.PublishStatus = "none"
	}
	if err := s.updateArticle(a); err != nil {
		s.writeError(w, r, 500, "internal_error", "归档失败", err.Error())
		return
	}
	s.writeJSON(w, 200, map[string]interface{}{"article": s.articleJSON(a, true)})
}

func (s *Server) handleDuplicate(w http.ResponseWriter, r *http.Request) {
	src, err := s.articleByUID(r.PathValue("uid"))
	if err != nil || src == nil {
		s.writeError(w, r, 404, "not_found", "文章不存在", nil)
		return
	}
	a := *src
	a.ID = 0
	a.UID = requireUID("art_")
	a.ExternalID = sql.NullString{}
	a.ClientID = sql.NullInt64{}
	a.Title = src.Title + "（副本）"
	a.Status = "draft"
	a.PublishStatus = "none"
	a.ScheduledAt = sql.NullTime{}
	a.FirstPublishedAt = sql.NullTime{}
	a.LastPublishedAt = sql.NullTime{}
	a.ContentHash = sql.NullString{}
	s.renderInto(&a)
	a.ContentHash = sql.NullString{String: contentHash(&a), Valid: true}
	if err := s.insertArticle(&a); err != nil {
		s.writeError(w, r, 500, "internal_error", "复制失败", err.Error())
		return
	}
	s.writeJSON(w, 201, map[string]interface{}{"article": s.articleJSON(&a, true)})
}

func (s *Server) handleManualPublish(w http.ResponseWriter, r *http.Request) {
	a, err := s.articleByUID(r.PathValue("uid"))
	if err != nil || a == nil {
		s.writeError(w, r, 404, "not_found", "文章不存在", nil)
		return
	}
	var in struct {
		Platform    string  `json:"platform"`
		Account     string  `json:"platform_account"`
		URL         string  `json:"url"`
		PostID      string  `json:"post_id"`
		PublishedAt *string `json:"published_at"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, r, 400, "invalid_json", "请求体不合法", nil)
		return
	}
	in.Platform = normalizePlatform(in.Platform)
	if err := validatePublished(&publishResult{Platform: in.Platform, Account: in.Account, URL: in.URL, PostID: in.PostID}); err != nil {
		s.writeError(w, r, 422, "validation_failed", err.Error(), nil)
		return
	}
	if !platformRe.MatchString(in.Platform) {
		s.writeError(w, r, 422, "validation_failed", "请选择或填写发布平台 code", nil)
		return
	}
	if strings.TrimSpace(in.Account) == "" {
		s.writeError(w, r, 422, "validation_failed", "请填写实际发布账号", nil)
		return
	}
	if !strings.HasPrefix(in.URL, "http://") && !strings.HasPrefix(in.URL, "https://") {
		s.writeError(w, r, 422, "validation_failed", "请填写 http(s) 链接", nil)
		return
	}
	var at interface{}
	if in.PublishedAt != nil && *in.PublishedAt != "" {
		t, err := time.Parse(time.RFC3339, *in.PublishedAt)
		if err != nil {
			s.writeError(w, r, 422, "validation_failed", "published_at 需为 RFC3339", nil)
			return
		}
		at = t.UTC()
	}
	if a.Status == "archived" {
		s.writeError(w, r, 409, "archived", "已归档文章不能标记发布", nil)
		return
	}
	_ = s.cancelActiveJob(a.ID, "manual_publish")
	snapshot, err := json.Marshal(a)
	if err != nil {
		s.db.record(err)
		return
	}
	uid := requireUID("job_")
	_, err = s.db.Exec(`INSERT INTO publish_jobs
		(uid, article_id, platform, status, active_key, attempts, max_attempts,
		 result_account, result_url, result_post_id, result_at, finished_at, created_at, updated_at,content_snapshot,review_status,review_next_at)
		VALUES (?,?,?,'succeeded',NULL,0,3,?,?,?,COALESCE(?,UTC_TIMESTAMP()),UTC_TIMESTAMP(),UTC_TIMESTAMP(),UTC_TIMESTAMP(),?,'pending',DATE_ADD(UTC_TIMESTAMP(),INTERVAL ? SECOND))`,
		uid, a.ID, in.Platform, in.Account, in.URL, nullStr(in.PostID), at, string(snapshot), s.settingInt("review.first_delay_seconds", 300))
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "记录发布失败", err.Error())
		return
	}
	_, _ = s.db.Exec(`UPDATE articles SET platform=?, publish_status='published', status='ready',revision=revision+1,review_status='pending',review_note='',review_checked_at=NULL,
		first_published_at=COALESCE(first_published_at,COALESCE(?,UTC_TIMESTAMP())),last_published_at=COALESCE(?,UTC_TIMESTAMP()),
		updated_at=UTC_TIMESTAMP() WHERE id=?`, in.Platform, at, at, a.ID)
	updated, _ := s.articleByUID(a.UID)
	s.writeJSON(w, 200, map[string]interface{}{"article": s.articleJSON(updated, true)})
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		BodyMD string `json:"body_md"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, r, 400, "invalid_json", "请求体不合法", nil)
		return
	}
	res := render.Render(in.BodyMD, func(uid string) (string, bool) { return s.media.URLByUID(uid) })
	s.writeJSON(w, 200, map[string]interface{}{
		"html": res.HTML, "text": res.Text, "chars": res.Chars, "minutes": res.Minutes, "images": res.Images,
	})
}

// ---------- admin queue ----------

func (s *Server) handleAdminQueue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
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
	where := []string{"1=1"}
	args := []any{}
	if v := q.Get("status"); v != "" {
		where = append(where, "j.status=?")
		args = append(args, v)
	}
	if v := q.Get("platform"); v != "" {
		where = append(where, "j.platform=?")
		args = append(args, strings.ToLower(v))
	}
	if v := q.Get("q"); v != "" {
		where = append(where, "(a.title LIKE ? OR j.uid LIKE ?)")
		args = append(args, "%"+v+"%", "%"+v+"%")
	}
	pageMode := q.Get("page") != ""
	if !pageMode {
		if v := q.Get("cursor"); v != "" {
			where = append(where, "j.id < ?")
			args = append(args, atoi(v, 1<<62))
		}
	}
	from := " FROM publish_jobs j LEFT JOIN articles a ON a.id=j.article_id WHERE " + strings.Join(where, " AND ")
	var total int
	if pageMode {
		if err := s.db.QueryRow("SELECT COUNT(*)"+from, args...).Scan(&total); err != nil {
			s.writeError(w, r, 500, "internal_error", "统计失败", err.Error())
			return
		}
		qargs := append(append([]any{}, args...), size, (page-1)*size)
		rows, err := s.db.Query("SELECT "+jobCols+from+" ORDER BY j.id DESC LIMIT ? OFFSET ?", qargs...)
		if err != nil {
			s.writeError(w, r, 500, "internal_error", "查询失败", err.Error())
			return
		}
		defer rows.Close()
		items := []*jobSummary{}
		for _, j := range collect(rows, scanJob) {
			items = append(items, s.jobSummaryOf(j))
		}
		s.writeJSON(w, 200, map[string]interface{}{"items": items, "total": total, "page": page, "page_size": size})
		return
	}
	args = append(args, limit)
	rows, err := s.db.Query("SELECT "+jobCols+from+" ORDER BY j.id DESC LIMIT ?", args...)
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

func (s *Server) handleAdminJob(w http.ResponseWriter, r *http.Request) {
	s.handleGetJob(w, r)
}

func (s *Server) handleRetry(w http.ResponseWriter, r *http.Request) {
	j, err := s.jobByUID(r.PathValue("uid"))
	if err != nil || j == nil {
		s.writeError(w, r, 404, "not_found", "任务不存在", nil)
		return
	}
	if j.Status != "failed_permanent" && j.Status != "failed_retryable" {
		s.writeError(w, r, 409, "invalid_state", "只有失败任务可以重试", map[string]string{"status": j.Status})
		return
	}
	if a, _ := s.articleByID(j.ArticleID); a == nil || a.Status != "ready" || a.PublishMode != "agent" || a.PublishStatus == "published" {
		s.writeError(w, r, 409, "invalid_state", "只有未发布且处于待发布状态的智能体文章可以重试", nil)
		return
	}
	if other, _ := s.activeJob(j.ArticleID); other != nil && other.ID != j.ID {
		s.writeError(w, r, 409, "invalid_state", "该文章已有活跃任务", map[string]string{"job": other.UID})
		return
	}
	_, err = s.db.Exec(`UPDATE publish_jobs SET status='queued', active_key=1, max_attempts=attempts+?, lease_owner=NULL,
		lease_expires_at=NULL, next_attempt_at=UTC_TIMESTAMP(), last_error_code=NULL, last_error_message=NULL,
		finished_at=NULL, updated_at=UTC_TIMESTAMP() WHERE id=? AND status IN ('failed_permanent','failed_retryable')`, s.settingInt("publish.max_attempts", 3), j.ID)
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "重试失败", err.Error())
		return
	}
	_, _ = s.db.Exec("UPDATE articles SET publish_status='queued',revision=revision+1, updated_at=UTC_TIMESTAMP() WHERE id=? AND publish_status <> 'published'", j.ArticleID)
	s.writeJSON(w, 200, map[string]interface{}{"ok": true})
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	j, err := s.jobByUID(r.PathValue("uid"))
	if err != nil || j == nil {
		s.writeError(w, r, 404, "not_found", "任务不存在", nil)
		return
	}
	if j.Status != "queued" {
		s.writeError(w, r, 409, "invalid_state", "只有 queued 任务可取消", map[string]string{"status": j.Status})
		return
	}
	_, err = s.db.Exec(`UPDATE publish_jobs SET status='cancelled', active_key=NULL, finished_at=UTC_TIMESTAMP(),
		cancel_reason='admin_cancel', updated_at=UTC_TIMESTAMP() WHERE id=? AND status='queued'`, j.ID)
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "取消失败", err.Error())
		return
	}
	_, _ = s.db.Exec("UPDATE articles SET publish_status='cancelled',revision=revision+1, updated_at=UTC_TIMESTAMP() WHERE id=? AND publish_status <> 'published'", j.ArticleID)
	s.writeJSON(w, 200, map[string]interface{}{"ok": true})
}

// ---------- clients ----------

func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, size := atoi(q.Get("page"), 1), atoi(q.Get("page_size"), 20)
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	var total int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM api_clients").Scan(&total); err != nil {
		s.writeError(w, r, 500, "internal_error", "统计失败", err.Error())
		return
	}
	rows, err := s.db.Query(`SELECT uid, name, type, scopes, token_prefix, token_cipher, status, last_used_at, created_at
		FROM api_clients ORDER BY id DESC LIMIT ? OFFSET ?`, size, (page-1)*size)
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "查询失败", err.Error())
		return
	}
	defer rows.Close()
	items := []map[string]interface{}{}
	for rows.Next() {
		var uid, name, typ, prefix string
		var scopes, cipherText []byte
		var status int
		var lastUsed sql.NullTime
		var created time.Time
		if rows.Scan(&uid, &name, &typ, &scopes, &prefix, &cipherText, &status, &lastUsed, &created) != nil {
			continue
		}
		var sc []string
		_ = json.Unmarshal(scopes, &sc)
		token := ""
		if len(cipherText) > 0 {
			if plain, err := s.decryptToken(cipherText); err == nil {
				token = plain
			}
		}
		items = append(items, map[string]interface{}{
			"uid": uid, "name": name, "type": typ, "scopes": sc, "token_prefix": prefix,
			"token": token, "token_viewable": token != "",
			"status": status, "last_used_at": timePtr(lastUsed), "created_at": created.UTC(),
		})
	}
	s.writeJSON(w, 200, map[string]interface{}{"items": items, "total": total, "page": page, "page_size": size})
}

func (s *Server) handleCreateClient(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name   string   `json:"name"`
		Type   string   `json:"type"`
		Scopes []string `json:"scopes"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		s.writeError(w, r, 422, "validation_failed", "name 必填", nil)
		return
	}
	if in.Type == "" {
		in.Type = "content"
	}
	if len(in.Scopes) == 0 {
		switch in.Type {
		case "publisher":
			in.Scopes = []string{"publish:claim", "publish:result"}
		case "reviewer":
			in.Scopes = []string{"review:queue", "review:report"}
		case "admin":
			in.Scopes = []string{"*"}
		default:
			in.Scopes = []string{"articles:write", "articles:read", "media:write"}
		}
	}
	token, prefix, hash := newToken()
	cipherText, err := s.encryptToken(token)
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "生成 Token 失败", err.Error())
		return
	}
	raw, _ := json.Marshal(in.Scopes)
	uid := requireUID("cli_")
	_, err = s.db.Exec(`INSERT INTO api_clients (uid, name, type, scopes, token_prefix, token_hash, token_cipher, status, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,1,UTC_TIMESTAMP(),UTC_TIMESTAMP())`, uid, in.Name, in.Type, string(raw), prefix, hash, cipherText)
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "创建失败", err.Error())
		return
	}
	s.writeJSON(w, 201, map[string]interface{}{
		"client": map[string]interface{}{"uid": uid, "name": in.Name, "type": in.Type, "scopes": in.Scopes, "token_prefix": prefix},
		"token":  token,
	})
}

func newToken() (token, prefix, hash string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	token = "wk_live_" + base64.RawURLEncoding.EncodeToString(b)
	prefix = token[:8]
	sum := sha256.Sum256([]byte(token))
	return token, prefix, hex.EncodeToString(sum[:])
}

func (s *Server) handleRotateClient(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	var id int64
	if err := s.db.QueryRow("SELECT id FROM api_clients WHERE uid=?", uid).Scan(&id); err != nil {
		s.writeError(w, r, 404, "not_found", "客户端不存在", nil)
		return
	}
	token, prefix, hash := newToken()
	cipherText, err := s.encryptToken(token)
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "生成 Token 失败", err.Error())
		return
	}
	_, err = s.db.Exec("UPDATE api_clients SET token_prefix=?, token_hash=?, token_cipher=?, updated_at=UTC_TIMESTAMP() WHERE id=?", prefix, hash, cipherText, id)
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "轮换失败", err.Error())
		return
	}
	s.writeJSON(w, 200, map[string]interface{}{"token": token, "token_prefix": prefix})
}

func (s *Server) handlePatchClient(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name   *string   `json:"name"`
		Scopes *[]string `json:"scopes"`
		Status *int      `json:"status"`
	}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, r, 400, "invalid_json", "请求体不合法", nil)
		return
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			s.writeError(w, r, 422, "validation_failed", "名称不能为空", nil)
			return
		}
		if utf8.RuneCountInString(name) > 100 {
			s.writeError(w, r, 422, "validation_failed", "名称不能超过 100 个字符", nil)
			return
		}
		in.Name = &name
	}
	uid := r.PathValue("uid")
	sets := []string{}
	args := []any{}
	if in.Name != nil {
		sets = append(sets, "name=?")
		args = append(args, *in.Name)
	}
	if in.Scopes != nil {
		raw, _ := json.Marshal(*in.Scopes)
		sets = append(sets, "scopes=?")
		args = append(args, string(raw))
	}
	if in.Status != nil {
		sets = append(sets, "status=?")
		args = append(args, *in.Status)
	}
	if len(sets) == 0 {
		s.writeError(w, r, 422, "validation_failed", "没有需要修改的字段", nil)
		return
	}
	args = append(args, uid)
	res, err := s.db.Exec("UPDATE api_clients SET "+strings.Join(sets, ", ")+", updated_at=UTC_TIMESTAMP() WHERE uid=?", args...)
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "更新失败", err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		s.writeError(w, r, 404, "not_found", "客户端不存在", nil)
		return
	}
	s.writeJSON(w, 200, map[string]interface{}{"ok": true})
}

// ---------- settings ----------

var settingKeys = map[string]string{
	"publish.lease_seconds":      "int",
	"publish.max_attempts":       "int",
	"publish.tick_interval":      "int",
	"api.rate_per_min":           "int",
	"media.max_upload_mb":        "int",
	"review.first_delay_seconds": "int",
	"review.interval_seconds":    "int",
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query("SELECT setting_key, setting_value FROM settings ORDER BY setting_key")
	if err != nil {
		s.writeError(w, r, 500, "internal_error", "查询失败", err.Error())
		return
	}
	defer rows.Close()
	out := map[string]interface{}{}
	for rows.Next() {
		var k string
		var raw []byte
		if rows.Scan(&k, &raw) == nil {
			var v interface{}
			if _, known := settingKeys[k]; known && json.Unmarshal(raw, &v) == nil {
				out[k] = v
			}
		}
	}
	s.writeJSON(w, 200, map[string]interface{}{"settings": out})
}

var settingRanges = map[string][2]int64{
	"publish.lease_seconds": {300, 3600}, "publish.max_attempts": {1, 10}, "publish.tick_interval": {10, 600}, "api.rate_per_min": {1, 10000}, "media.max_upload_mb": {1, 25},
	"review.first_delay_seconds": {30, 86400}, "review.interval_seconds": {300, 604800},
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var in map[string]interface{}
	if err := decodeJSON(r, &in); err != nil {
		s.writeError(w, r, 400, "invalid_json", "请求体不合法", nil)
		return
	}
	for k, v := range in {
		bounds, ok := settingRanges[k]
		n, isNumber := v.(float64)
		if !ok || !isNumber || math.Trunc(n) != n || n < float64(bounds[0]) || n > float64(bounds[1]) {
			s.writeError(w, r, 422, "validation_failed", "设置项不存在或数值超出范围："+k, nil)
			return
		}
	}
	for k, v := range in {
		raw, _ := json.Marshal(v)
		if _, err := s.db.Exec(`INSERT INTO settings(setting_key,setting_value,updated_at) VALUES (?,?,UTC_TIMESTAMP()) ON DUPLICATE KEY UPDATE setting_value=VALUES(setting_value),updated_at=UTC_TIMESTAMP()`, k, string(raw)); err != nil {
			return
		}
	}
	s.writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- admin page ----------

func (s *Server) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/admin")
	if path == "" || path == "/" {
		path = "/index.html"
	}
	data, err := adminFS.ReadFile("dist" + path)
	if err != nil {
		data, err = adminFS.ReadFile("dist/index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, path, time.Time{}, strings.NewReader(string(data)))
}

var _ = fmt.Sprint
