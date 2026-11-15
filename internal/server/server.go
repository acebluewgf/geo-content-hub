package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"

	"contenthub/internal/config"
	"contenthub/internal/media"
)

type Server struct {
	cfg      *config.Config
	db       *database
	media    *media.Store
	start    time.Time
	lastTick *atomic.Int64
	ticking  *atomic.Bool
	limiter  *rateLimiter
}

func New(cfg *config.Config, db *sql.DB) *Server {
	return &Server{
		cfg:      cfg,
		db:       &database{pool: db},
		lastTick: &atomic.Int64{}, ticking: &atomic.Bool{},
		media:   &media.Store{DB: db, Dir: cfg.MediaDir, BaseURL: cfg.BaseURL},
		start:   time.Now(),
		limiter: newRateLimiter(),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, r *http.Request) { s.writeJSON(w, 200, map[string]bool{"alive": true}) })
	mux.HandleFunc("GET /readyz", s.readOnly((*Server).handleHealth))
	mux.HandleFunc("GET /healthz", s.readOnly((*Server).handleHealth))

	mux.HandleFunc("POST /api/v1/articles", s.requireAPI("articles:write", s.mutateIdempotent((*Server).handleCreateArticle)))
	mux.HandleFunc("GET /api/v1/articles", s.requireAPI("articles:read", s.readOnly((*Server).handleListArticles)))
	mux.HandleFunc("GET /api/v1/articles/{uid}", s.requireAPI("articles:read", s.readOnly((*Server).handleGetArticle)))
	mux.HandleFunc("PATCH /api/v1/articles/{uid}", s.requireAPI("articles:write", s.mutate((*Server).handlePatchArticle)))
	mux.HandleFunc("POST /api/v1/articles/{uid}/schedule", s.requireAPI("articles:write", s.mutate((*Server).handleSchedule)))
	mux.HandleFunc("POST /api/v1/articles/{uid}/cancel", s.requireAPI("articles:write", s.mutate((*Server).handleCancelArticle)))

	mux.HandleFunc("POST /api/v1/media", s.requireAPI("media:write", s.readOnly((*Server).handleUploadMedia)))
	mux.HandleFunc("POST /api/v1/media/from-url", s.requireAPI("media:write", s.readOnly((*Server).handleMediaFromURL)))

	mux.HandleFunc("GET /api/v1/publish/queue", s.requireAPI("publish:claim", s.readOnly((*Server).handleQueue)))
	mux.HandleFunc("POST /api/v1/publish/claim", s.requireAPI("publish:claim", s.mutate((*Server).handleClaim)))
	mux.HandleFunc("GET /api/v1/publish/{uid}", s.requireAPI("publish:claim", s.readOnly((*Server).handleGetJob)))
	mux.HandleFunc("POST /api/v1/publish/{uid}/heartbeat", s.requireAPI("publish:claim", s.mutate((*Server).handleHeartbeat)))
	mux.HandleFunc("POST /api/v1/publish/{uid}/result", s.requireAPI("publish:result", s.mutateIdempotent((*Server).handleResult)))
	mux.HandleFunc("GET /api/v1/publish/review/queue", s.requireAPIAny([]string{"publish:claim", "review:queue"}, s.readOnly((*Server).handleReviewQueue)))
	mux.HandleFunc("POST /api/v1/publish/review/claim", s.requireAPIAny([]string{"publish:claim", "review:queue"}, s.mutate((*Server).handleReviewClaim)))
	mux.HandleFunc("POST /api/v1/publish/review/heartbeat", s.requireAPIAny([]string{"publish:claim", "review:queue"}, s.mutate((*Server).handleReviewHeartbeat)))
	mux.HandleFunc("POST /api/v1/publish/{uid}/review", s.requireAPIAny([]string{"publish:result", "review:report"}, s.mutate((*Server).handleReviewReport)))

	mux.HandleFunc("POST /admin/api/login", s.readOnly((*Server).handleLogin))
	mux.HandleFunc("POST /admin/api/logout", s.adminAuth(s.readOnly((*Server).handleLogout)))
	mux.HandleFunc("GET /admin/api/me", s.adminAuth(s.readOnly((*Server).handleMe)))
	mux.HandleFunc("GET /admin/api/csrf", s.readOnly((*Server).handleCSRF))
	mux.HandleFunc("GET /admin/api/dashboard", s.adminAuth(s.readOnly((*Server).handleDashboard)))

	mux.HandleFunc("GET /admin/api/articles", s.adminAuth(s.readOnly((*Server).handleAdminArticles)))
	mux.HandleFunc("GET /admin/api/articles/stats", s.adminAuth(s.readOnly((*Server).handleArticleStats)))
	mux.HandleFunc("POST /admin/api/articles", s.adminEditor(s.mutateIdempotent((*Server).handleAdminCreateArticle)))
	mux.HandleFunc("GET /admin/api/articles/{uid}", s.adminAuth(s.readOnly((*Server).handleAdminArticle)))
	mux.HandleFunc("PATCH /admin/api/articles/{uid}", s.adminEditor(s.mutate((*Server).handleAdminPatchArticle)))
	mux.HandleFunc("POST /admin/api/articles/{uid}/schedule", s.adminEditor(s.mutate((*Server).handleSchedule)))
	mux.HandleFunc("POST /admin/api/articles/{uid}/cancel", s.adminEditor(s.mutate((*Server).handleCancelArticle)))
	mux.HandleFunc("POST /admin/api/articles/{uid}/archive", s.adminOnly(s.mutate((*Server).handleArchive)))
	mux.HandleFunc("POST /admin/api/articles/{uid}/duplicate", s.adminEditor(s.mutate((*Server).handleDuplicate)))
	mux.HandleFunc("POST /admin/api/articles/{uid}/manual-publish", s.adminEditor(s.mutate((*Server).handleManualPublish)))
	mux.HandleFunc("POST /admin/api/preview", s.adminAuth(s.readOnly((*Server).handlePreview)))
	mux.HandleFunc("POST /admin/api/media", s.adminEditor(s.readOnly((*Server).handleUploadMedia)))

	mux.HandleFunc("GET /admin/api/queue", s.adminAuth(s.readOnly((*Server).handleAdminQueue)))
	mux.HandleFunc("GET /admin/api/queue/{uid}", s.adminAuth(s.readOnly((*Server).handleAdminJob)))
	mux.HandleFunc("POST /admin/api/queue/{uid}/reconcile", s.adminEditor(s.mutate((*Server).handleReconcile)))
	mux.HandleFunc("POST /admin/api/queue/{uid}/retry", s.adminEditor(s.mutate((*Server).handleRetry)))
	mux.HandleFunc("POST /admin/api/queue/{uid}/cancel", s.adminEditor(s.mutate((*Server).handleCancelJob)))

	mux.HandleFunc("GET /admin/api/clients", s.adminOnly(s.readOnly((*Server).handleClients)))
	mux.HandleFunc("POST /admin/api/clients", s.adminOnly(s.mutate((*Server).handleCreateClient)))
	mux.HandleFunc("POST /admin/api/clients/{uid}/rotate", s.adminOnly(s.mutate((*Server).handleRotateClient)))
	mux.HandleFunc("PATCH /admin/api/clients/{uid}", s.adminOnly(s.mutate((*Server).handlePatchClient)))

	mux.HandleFunc("GET /admin/api/settings", s.adminOnly(s.readOnly((*Server).handleGetSettings)))
	mux.HandleFunc("PUT /admin/api/settings", s.adminOnly(s.mutate((*Server).handlePutSettings)))

	mux.Handle("GET /media/", http.StripPrefix("/media/", http.FileServer(http.Dir(s.cfg.MediaDir))))
	mux.HandleFunc("/admin", s.readOnly((*Server).handleAdminPage))
	mux.HandleFunc("/admin/", s.readOnly((*Server).handleAdminPage))
	mux.HandleFunc("/", s.readOnly((*Server).handleRoot))

	return s.recoverer(s.requestID(s.logger(s.csrf(mux))))
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/", http.StatusFound)
}

// ---------- middleware ----------

type ctxKey string

const (
	ctxClient  ctxKey = "client"
	ctxUser    ctxKey = "user"
	ctxRequest ctxKey = "request_id"
)

type apiClient struct {
	ID     int64
	UID    string
	Name   string
	Type   string
	Scopes []string
}

type sessionUser struct {
	UID      string `json:"uid"`
	Username string `json:"username"`
	Display  string `json:"display"`
	Role     string `json:"role"`
	Exp      int64  `json:"exp"`
}

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" || len(id) > 64 {
			id = ulid.Make().String()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxRequest, id)))
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				slog.Error("panic", "path", r.URL.Path, "panic", v)
				s.writeError(w, r, http.StatusInternalServerError, "internal_error", "服务内部错误", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		slog.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"ms", time.Since(start).Milliseconds(), "request_id", requestID(r))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func requestID(r *http.Request) string {
	if v, ok := r.Context().Value(ctxRequest).(string); ok {
		return v
	}
	return ""
}

func (s *Server) requireAPI(scope string, next http.HandlerFunc) http.HandlerFunc {
	return s.requireAPIAny([]string{scope}, next)
}

func (s *Server) requireAPIAny(scopes []string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			s.writeError(w, r, http.StatusUnauthorized, "unauthorized", "缺少 Bearer Token", nil)
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		sum := sha256.Sum256([]byte(token))
		hash := hex.EncodeToString(sum[:])

		var c apiClient
		var scopesRaw []byte
		authDB := &database{pool: s.db.pool, ctx: r.Context()}
		err := authDB.QueryRow(`SELECT id, uid, name, type, scopes FROM api_clients WHERE token_hash = ? AND status = 1`, hash).
			Scan(&c.ID, &c.UID, &c.Name, &c.Type, &scopesRaw)
		if err == sql.ErrNoRows {
			s.writeError(w, r, http.StatusUnauthorized, "unauthorized", "Token 无效或已停用", nil)
			return
		}
		if err != nil {
			s.writeError(w, r, 500, "internal_error", "查询客户端失败", nil)
			return
		}
		_ = json.Unmarshal(scopesRaw, &c.Scopes)
		allowed := false
		for _, scope := range scopes {
			if hasScope(c.Scopes, scope) {
				allowed = true
				break
			}
		}
		if !allowed {
			s.writeError(w, r, http.StatusForbidden, "forbidden_scope", "缺少权限 "+strings.Join(scopes, " 或 "), nil)
			return
		}
		if !s.limiter.allow(c.ID, s.settingInt("api.rate_per_min", 120)) {
			w.Header().Set("Retry-After", "60")
			s.writeError(w, r, http.StatusTooManyRequests, "rate_limited", "请求过于频繁", nil)
			return
		}
		_, _ = authDB.Exec("UPDATE api_clients SET last_used_at = UTC_TIMESTAMP() WHERE id = ?", c.ID)
		ctx := context.WithValue(r.Context(), ctxClient, &c)
		next(w, r.WithContext(ctx))
	}
}

func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want || s == "*" {
			return true
		}
	}
	return false
}

func client(r *http.Request) *apiClient {
	c, _ := r.Context().Value(ctxClient).(*apiClient)
	return c
}

// ---------- session ----------

func tokenDigest(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}
func newLeaseToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func (s *Server) setSession(w http.ResponseWriter, r *http.Request, u *sessionUser, passwordHash string) error {
	token := newLeaseToken()
	u.Exp = time.Now().Add(7 * 24 * time.Hour).Unix()
	if _, err := s.db.Exec(`INSERT INTO user_sessions (token_hash,user_uid,credential_hash,expires_at) VALUES (?,?,?,?)`, tokenDigest(token), u.UID, tokenDigest(passwordHash), time.Unix(u.Exp, 0).UTC()); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "ch_session", Value: token, Path: "/", HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600})
	return nil
}
func (s *Server) currentUser(r *http.Request) *sessionUser {
	c, err := r.Cookie("ch_session")
	if err != nil || len(c.Value) != 64 {
		return nil
	}
	var u sessionUser
	var hash, credential string
	var expiry time.Time
	db := &database{pool: s.db.pool, ctx: r.Context()}
	err = db.QueryRow(`SELECT u.uid,u.username,u.display_name,u.role,u.password_hash,s.credential_hash,s.expires_at
 FROM user_sessions s JOIN users u ON u.uid=s.user_uid WHERE s.token_hash=? AND s.expires_at>UTC_TIMESTAMP() AND u.status=1`, tokenDigest(c.Value)).Scan(&u.UID, &u.Username, &u.Display, &u.Role, &hash, &credential, &expiry)
	if err != nil || credential != tokenDigest(hash) {
		return nil
	}
	u.Exp = expiry.Unix()
	return &u
}

func (s *Server) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "ch_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}

func (s *Server) adminAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := s.currentUser(r)
		if u == nil {
			s.writeError(w, r, http.StatusUnauthorized, "unauthorized", "请先登录", nil)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxUser, u)))
	}
}

func (s *Server) adminEditor(next http.HandlerFunc) http.HandlerFunc {
	return s.adminAuth(func(w http.ResponseWriter, r *http.Request) {
		u := user(r)
		if u.Role == "viewer" {
			s.writeError(w, r, http.StatusForbidden, "forbidden", "当前账号无编辑权限", nil)
			return
		}
		next(w, r)
	})
}

func (s *Server) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return s.adminAuth(func(w http.ResponseWriter, r *http.Request) {
		if u := user(r); u.Role != "admin" {
			s.writeError(w, r, http.StatusForbidden, "forbidden", "仅管理员可操作", nil)
			return
		}
		next(w, r)
	})
}

func user(r *http.Request) *sessionUser {
	u, _ := r.Context().Value(ctxUser).(*sessionUser)
	return u
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin") || strings.HasPrefix(r.URL.Path, "/api/") {
			if _, err := r.Cookie("ch_csrf"); err != nil {
				b := make([]byte, 18)
				_, _ = rand.Read(b)
				http.SetCookie(w, &http.Cookie{
					Name: "ch_csrf", Value: base64.RawURLEncoding.EncodeToString(b), Path: "/",
					Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600,
				})
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions &&
				strings.HasPrefix(r.URL.Path, "/admin/api/") && r.URL.Path != "/admin/api/login" {
				c, _ := r.Cookie("ch_csrf")
				if c == nil || c.Value == "" || r.Header.Get("X-CSRF-Token") != c.Value {
					s.writeError(w, r, http.StatusForbidden, "csrf_failed", "CSRF 校验失败，请刷新页面", nil)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- responses ----------

type apiError struct {
	Error struct {
		Code    string      `json:"code"`
		Message string      `json:"message"`
		Details interface{} `json:"details,omitempty"`
	} `json:"error"`
	RequestID string `json:"request_id"`
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, code, msg string, details interface{}) {
	if status >= 500 {
		slog.Error("request failed", "code", code, "msg", msg, "path", r.URL.Path, "request_id", requestID(r), "details", details)
		details = nil
	}
	var e apiError
	e.Error.Code = code
	e.Error.Message = msg
	e.Error.Details = details
	e.RequestID = requestID(r)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(e)
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSON(r *http.Request, v interface{}) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, (2<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 2<<20 {
		return errors.New("request body exceeds 2 MiB")
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err = dec.Decode(v); err != nil {
		return err
	}
	if err = dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("expected one JSON value")
	}
	return nil
}

// ---------- rate limiter ----------

type window struct {
	start time.Time
	count int
}

type rateLimiter struct {
	mu  sync.Mutex
	per map[int64]*window
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{per: map[int64]*window{}}
}

func (l *rateLimiter) allow(clientID int64, limit int) bool {
	if limit <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	w := l.per[clientID]
	if w == nil || now.Sub(w.start) >= time.Minute {
		l.per[clientID] = &window{start: now, count: 1}
		return true
	}
	if w.count >= limit {
		return false
	}
	w.count++
	return true
}

// ---------- misc ----------

func parseTimePtr(v *string) (*time.Time, error) {
	if v == nil || *v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, *v)
	if err != nil {
		return nil, err
	}
	t = t.UTC()
	return &t, nil
}

func fmtErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func requireUID(prefix string) string {
	return prefix + ulid.Make().String()
}

var errNotFound = errors.New("not found")

func (s *Server) mediaURL(path string) string {
	return strings.TrimRight(s.cfg.BaseURL, "/") + "/media/" + path
}

func atoi(s string, def int) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

var _ = os.Getenv
var _ = fmt.Errorf
