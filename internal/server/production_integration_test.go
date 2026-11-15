package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"contenthub/internal/config"
	"contenthub/internal/migrate"
	"contenthub/migrations"
	"github.com/go-sql-driver/mysql"
	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/bcrypt"
)

type fixture struct {
	s       *Server
	db      *sql.DB
	handler http.Handler
	token   string
}

func integration(t *testing.T) *fixture {
	return integrationSeeded(t, nil)
}
func integrationSeeded(t *testing.T, beforeMigrate func(*sql.DB)) *fixture {
	t.Helper()
	dsn := os.Getenv("CONTENTHUB_TEST_DSN")
	if dsn == "" {
		t.Skip("set CONTENTHUB_TEST_DSN to an isolated loopback MySQL instance")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	host, _, err := net.SplitHostPort(cfg.Addr)
	ip := net.ParseIP(host)
	if err != nil || cfg.Net != "tcp" || ip == nil || !ip.IsLoopback() || cfg.DBName != "" {
		t.Fatal("test DSN must use loopback TCP and no database; tests create their own schema")
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Timeout = 2 * time.Second
	cfg.ReadTimeout = 10 * time.Second
	cfg.WriteTimeout = 10 * time.Second
	cfg.Collation = "utf8mb4_unicode_ci"
	cfg.Params = map[string]string{"charset": "utf8mb4", "time_zone": "'+00:00'", "sql_mode": "'STRICT_TRANS_TABLES,ONLY_FULL_GROUP_BY,NO_ENGINE_SUBSTITUTION'"}
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	schema := "contenthub_test_" + strings.ToLower(ulid.Make().String())
	if _, err = admin.Exec("CREATE DATABASE " + schema + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	cfg.DBName = schema
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(24)
	t.Cleanup(func() {
		db.Close()
		if strings.HasPrefix(schema, "contenthub_test_") {
			_, _ = admin.Exec("DROP DATABASE " + schema)
		}
		admin.Close()
	})
	if beforeMigrate != nil {
		beforeMigrate(db)
	}
	if err = migrate.Apply(db); err != nil {
		t.Fatal(err)
	}
	if err = migrate.Apply(db); err != nil {
		t.Fatalf("migration replay: %v", err)
	}
	if err = migrate.Check(db); err != nil {
		t.Fatal(err)
	}
	if err = migrate.CheckCurrent(db); err != nil {
		t.Fatal(err)
	}
	token := "test-only-" + ulid.Make().String()
	_, err = db.Exec(`INSERT INTO api_clients(uid,name,type,token_prefix,token_hash,scopes) VALUES (?,'test','admin','testonly',?,'["*"]')`, ulid.Make().String(), tokenDigest(token))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("UPDATE settings SET setting_value='10000' WHERE setting_key='api.rate_per_min'")
	if err != nil {
		t.Fatal(err)
	}
	s := New(&config.Config{SessionSecret: strings.Repeat("test", 12), BaseURL: "https://audit.invalid", MediaDir: t.TempDir()}, db)
	return &fixture{s, db, s.Handler(), token}
}

func TestProductionUpgradeFromLegacyWithData(t *testing.T) {
	f := integrationSeeded(t, func(db *sql.DB) {
		execSQL(t, db, `CREATE TABLE schema_migrations(version VARCHAR(64) NOT NULL PRIMARY KEY,checksum CHAR(64) NOT NULL,applied_at DATETIME DEFAULT CURRENT_TIMESTAMP) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC`)
		entries, err := migrations.FS.ReadDir(".")
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") || entry.Name() >= "014" {
				continue
			}
			raw, err := migrations.FS.ReadFile(entry.Name())
			if err != nil {
				t.Fatal(err)
			}
			for _, stmt := range strings.Split(string(raw), ";") {
				if strings.TrimSpace(stmt) != "" {
					execSQL(t, db, stmt)
				}
			}
			execSQL(t, db, "INSERT INTO schema_migrations(version,checksum) VALUES (?,?)", entry.Name(), fmt.Sprintf("%x", sha256.Sum256(raw)))
		}
		execSQL(t, db, `INSERT INTO articles(uid,title,body_md,body_html,body_text,status,publish_status) VALUES ('art_legacy','Legacy title','Legacy body','<p>Legacy body</p>','Legacy body','ready','publishing')`)
		execSQL(t, db, `INSERT INTO publish_jobs(uid,article_id,status,active_key,lease_owner,lease_expires_at,attempts) SELECT 'job_legacy',id,'claimed',1,'old-worker',DATE_SUB(UTC_TIMESTAMP(),INTERVAL 1 SECOND),1 FROM articles WHERE uid='art_legacy'`)
	})
	a := f.article(t, "art_legacy")
	if a["revision"] != float64(1) || a["content_md"] != "Legacy body" {
		t.Fatalf("legacy content damaged: %v", a)
	}
	f.s.tick()
	j := decoded(t, request(f.handler, "GET", "/api/v1/publish/job_legacy", nil, f.token, "", nil), 200)["job"].(map[string]any)
	if j["status"] != "uncertain" || j["snapshot_available"] != false || j["body_md"] != "" {
		t.Fatalf("legacy task incorrectly recovered: %v", j)
	}
	if count(t, f.db, "SELECT COUNT(*) FROM publish_jobs WHERE status='queued'") != 0 {
		t.Fatal("legacy publication automatically retried")
	}
}
func request(h http.Handler, method, path string, body any, token, key string, cookie *http.Cookie) *httptest.ResponseRecorder {
	var data []byte
	if body != nil {
		data, _ = json.Marshal(body)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	if cookie != nil {
		r.AddCookie(cookie)
		r.AddCookie(&http.Cookie{Name: "ch_csrf", Value: "test-csrf"})
		r.Header.Set("X-CSRF-Token", "test-csrf")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func decoded(t *testing.T, w *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status=%d want=%d body=%s", w.Code, status, w.Body.String())
	}
	var d map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	return d
}
func (f *fixture) create(t *testing.T, extra map[string]any) map[string]any {
	t.Helper()
	body := map[string]any{"title": "Original title", "body_md": "Original body"}
	for k, v := range extra {
		body[k] = v
	}
	return decoded(t, request(f.handler, "POST", "/api/v1/articles", body, f.token, ulid.Make().String(), nil), 201)["article"].(map[string]any)
}
func (f *fixture) article(t *testing.T, uid string) map[string]any {
	t.Helper()
	return decoded(t, request(f.handler, "GET", "/api/v1/articles/"+uid, nil, f.token, "", nil), 200)["article"].(map[string]any)
}
func (f *fixture) claim(t *testing.T) map[string]any {
	t.Helper()
	d := decoded(t, request(f.handler, "POST", "/api/v1/publish/claim", map[string]any{"worker_id": "worker", "lease_protocol": 2}, f.token, "", nil), 200)
	jobs := d["jobs"].([]any)
	if len(jobs) != 1 {
		t.Fatalf("jobs=%v", jobs)
	}
	return jobs[0].(map[string]any)
}
func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func execSQL(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func (f *fixture) login(t *testing.T, role string) *http.Cookie {
	t.Helper()
	uid := ulid.Make().String()
	hash, _ := bcrypt.GenerateFromPassword([]byte("test-password-123"), bcrypt.MinCost)
	execSQL(t, f.db, `INSERT INTO users(uid,username,password_hash,display_name,role) VALUES (?,?,?,'Tester',?)`, uid, uid, string(hash), role)
	w := request(f.handler, "POST", "/admin/api/login", map[string]any{"username": uid, "password": "test-password-123"}, "", "", nil)
	decoded(t, w, 200)
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "ch_session" {
			return cookie
		}
	}
	t.Fatal("missing session")
	return nil
}
func successBody(j map[string]any) map[string]any {
	return map[string]any{"status": "success", "worker_id": "worker", "lease_token": j["lease_token"], "platform": "wechat", "platform_account": "test-account", "url": "https://example.com/published"}
}

func TestProductionIdempotencyAcrossInstances(t *testing.T) {
	f := integration(t)
	other := New(f.s.cfg, f.db).Handler()
	body := map[string]any{"title": "same", "body_md": "same"}
	results := make(chan *httptest.ResponseRecorder, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h := f.handler
			if i%2 == 0 {
				h = other
			}
			results <- request(h, "POST", "/api/v1/articles", body, f.token, "same-key", nil)
		}(i)
	}
	wg.Wait()
	close(results)
	uid := ""
	for w := range results {
		a := decoded(t, w, 201)["article"].(map[string]any)
		if uid == "" {
			uid = a["uid"].(string)
		} else if uid != a["uid"] {
			t.Fatal("duplicate article")
		}
	}
	for _, table := range []string{"articles", "publish_jobs", "idempotency_keys", "audit_events"} {
		if count(t, f.db, "SELECT COUNT(*) FROM "+table) != 1 {
			t.Fatalf("unexpected rows in %s", table)
		}
	}
	decoded(t, request(f.handler, "POST", "/api/v1/articles", map[string]any{"title": "different", "body_md": "same"}, f.token, "same-key", nil), 409)
	execSQL(t, f.db, "UPDATE idempotency_keys SET expires_at=DATE_SUB(UTC_TIMESTAMP(),INTERVAL 1 HOUR)")
	decoded(t, request(f.handler, "POST", "/api/v1/articles", body, f.token, "same-key", nil), 201)
	if count(t, f.db, "SELECT COUNT(*) FROM articles") != 2 {
		t.Fatal("expired key was not reusable")
	}
	decoded(t, request(f.handler, "POST", "/api/v1/articles", body, f.token, "", nil), 428)
}

func TestProductionQueueFailureRollsBackEverything(t *testing.T) {
	f := integration(t)
	execSQL(t, f.db, `CREATE TRIGGER fail_queue BEFORE INSERT ON publish_jobs FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected failure'`)
	body := map[string]any{"title": "test", "body_md": "body"}
	decoded(t, request(f.handler, "POST", "/api/v1/articles", body, f.token, "retry-key", nil), 503)
	for _, table := range []string{"articles", "publish_jobs", "idempotency_keys", "audit_events"} {
		if count(t, f.db, "SELECT COUNT(*) FROM "+table) != 0 {
			t.Fatalf("partial commit in %s", table)
		}
	}
	execSQL(t, f.db, "DROP TRIGGER fail_queue")
	decoded(t, request(f.handler, "POST", "/api/v1/articles", body, f.token, "retry-key", nil), 201)
}

func TestProductionRevisionUpsertAndCoverClear(t *testing.T) {
	f := integration(t)
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4)))
	m, err := f.s.media.SaveBytes(buf.Bytes(), "upload", 1)
	if err != nil {
		t.Fatal(err)
	}
	a := f.create(t, map[string]any{"external_id": "external-one", "cover": map[string]any{"uid": m.UID}})
	uid := a["uid"].(string)
	body := map[string]any{"external_id": "external-one", "title": "Original title", "body_md": "Original body", "status": "draft", "publish_mode": "manual", "revision": a["revision"]}
	a = decoded(t, request(f.handler, "POST", "/api/v1/articles", body, f.token, "upsert", nil), 200)["article"].(map[string]any)
	if a["status"] != "draft" || a["publish_mode"] != "manual" || a["publish_status"] != "none" {
		t.Fatalf("intent ignored: %v", a)
	}
	rev := a["revision"]
	a = decoded(t, request(f.handler, "PATCH", "/api/v1/articles/"+uid, map[string]any{"cover": nil, "revision": rev}, f.token, "", nil), 200)["article"].(map[string]any)
	if a["cover"] != nil {
		t.Fatal("cover not cleared")
	}
	decoded(t, request(f.handler, "PATCH", "/api/v1/articles/"+uid, map[string]any{"title": "stale", "revision": rev}, f.token, "", nil), 409)
	decoded(t, request(f.handler, "PATCH", "/api/v1/articles/"+uid, map[string]any{"title": "missing revision"}, f.token, "", nil), 428)
	body["revision"] = a["revision"]
	body["status"] = "ready"
	body["publish_mode"] = "agent"
	body["scheduled_at"] = time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	a = decoded(t, request(f.handler, "POST", "/api/v1/articles", body, f.token, "schedule-only", nil), 200)["article"].(map[string]any)
	if a["publish_status"] != "scheduled" {
		t.Fatalf("schedule ignored: %v", a)
	}
	delete(body, "scheduled_at")
	delete(body, "status")
	body["revision"] = a["revision"]
	a = decoded(t, request(f.handler, "POST", "/api/v1/articles", body, f.token, "preserve-schedule", nil), 200)["article"].(map[string]any)
	if a["publish_status"] != "scheduled" || a["status"] != "ready" {
		t.Fatalf("omitted fields changed intent: %v", a)
	}
}

func TestProductionLeaseSnapshotAndUnknownOutcome(t *testing.T) {
	f := integration(t)
	a := f.create(t, nil)
	uid := a["uid"].(string)
	decoded(t, request(f.handler, "POST", "/api/v1/publish/claim", map[string]any{"worker_id": "old-client"}, f.token, "", nil), 426)
	j := f.claim(t)
	jobUID := j["uid"].(string)
	if j["lease_token"] == "" || j["snapshot_available"] != true {
		t.Fatal("missing token/snapshot")
	}
	for i := 0; i < 2; i++ {
		decoded(t, request(f.handler, "POST", "/api/v1/publish/"+jobUID+"/heartbeat", map[string]any{"worker_id": "worker", "lease_token": j["lease_token"]}, f.token, "", nil), 200)
	}
	a = f.article(t, uid)
	decoded(t, request(f.handler, "PATCH", "/api/v1/articles/"+uid, map[string]any{"title": "changed", "revision": a["revision"]}, f.token, "", nil), 409)
	decoded(t, request(f.handler, "POST", "/api/v1/articles/"+uid+"/cancel", nil, f.token, "", nil), 409)
	execSQL(t, f.db, "UPDATE publish_jobs SET lease_expires_at=DATE_SUB(UTC_TIMESTAMP(),INTERVAL 1 SECOND) WHERE uid=?", jobUID)
	decoded(t, request(f.handler, "POST", "/api/v1/publish/"+jobUID+"/heartbeat", map[string]any{"worker_id": "worker", "lease_token": j["lease_token"]}, f.token, "", nil), 409)
	f.s.tick()
	if f.article(t, uid)["publish_status"] != "uncertain" {
		t.Fatal("expired lease must require reconciliation")
	}
	jobs := decoded(t, request(f.handler, "POST", "/api/v1/publish/claim", map[string]any{"worker_id": "worker", "lease_protocol": 2}, f.token, "", nil), 200)["jobs"].([]any)
	if len(jobs) != 0 {
		t.Fatal("uncertain task was automatically retried")
	}
	admin := f.login(t, "admin")
	decoded(t, request(f.handler, "POST", "/admin/api/queue/"+jobUID+"/retry", nil, "", "", admin), 409)
	decoded(t, request(f.handler, "POST", "/admin/api/queue/"+jobUID+"/reconcile", map[string]any{"resolution": "not_published", "note": "Checked the platform; no post exists"}, "", "", admin), 200)
	decoded(t, request(f.handler, "POST", "/admin/api/queue/"+jobUID+"/retry", nil, "", "", admin), 200)
	next := f.claim(t)
	if next["lease_token"] == j["lease_token"] {
		t.Fatal("lease token reused")
	}
	decoded(t, request(f.handler, "POST", "/api/v1/publish/"+jobUID+"/result", successBody(j), f.token, "stale-attempt", nil), 409)
	body := successBody(next)
	decoded(t, request(f.handler, "POST", "/api/v1/publish/"+jobUID+"/result", body, f.token, "success", nil), 200)
	decoded(t, request(f.handler, "POST", "/api/v1/publish/"+jobUID+"/result", body, f.token, "success", nil), 200)
	a = f.article(t, uid)
	decoded(t, request(f.handler, "PATCH", "/api/v1/articles/"+uid, map[string]any{"title": "Revised title", "body_md": "Revised body", "revision": a["revision"]}, f.token, "", nil), 200)
	historical := decoded(t, request(f.handler, "GET", "/api/v1/publish/"+jobUID, nil, f.token, "", nil), 200)["job"].(map[string]any)
	if historical["body_md"] != "Original body" || historical["title"] != "Original title" {
		t.Fatal("snapshot changed after editing")
	}
	if count(t, f.db, "SELECT COUNT(*) FROM job_attempts") != 2 {
		t.Fatal("attempt history lost")
	}
}

func TestProductionResultFailureRollsBackAndFencesClient(t *testing.T) {
	f := integration(t)
	f.create(t, nil)
	j := f.claim(t)
	path := "/api/v1/publish/" + j["uid"].(string) + "/result"
	second := "second-client-token"
	execSQL(t, f.db, `INSERT INTO api_clients(uid,name,type,token_prefix,token_hash,scopes) VALUES (?,'other','publisher','testonly',?,'["publish:result"]')`, ulid.Make().String(), tokenDigest(second))
	decoded(t, request(f.handler, "POST", path, successBody(j), second, "foreign", nil), 409)
	execSQL(t, f.db, `CREATE TRIGGER fail_article_update BEFORE UPDATE ON articles FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected failure'`)
	decoded(t, request(f.handler, "POST", path, successBody(j), f.token, "result", nil), 503)
	if count(t, f.db, "SELECT COUNT(*) FROM publish_jobs WHERE status='claimed'") != 1 {
		t.Fatal("partial result commit")
	}
	execSQL(t, f.db, "DROP TRIGGER fail_article_update")
	decoded(t, request(f.handler, "POST", path, successBody(j), f.token, "result", nil), 200)
}

func TestProductionSessionsAndRoleRevocation(t *testing.T) {
	f := integration(t)
	for _, role := range []string{"admin", "editor", "viewer"} {
		t.Run(role, func(t *testing.T) {
			cookie := f.login(t, role)
			decoded(t, request(f.handler, "POST", "/admin/api/logout", nil, "", "", cookie), 200)
			decoded(t, request(f.handler, "GET", "/admin/api/me", nil, "", "", cookie), 401)
		})
	}
	cookie := f.login(t, "admin")
	execSQL(t, f.db, "UPDATE users SET role='viewer'")
	decoded(t, request(f.handler, "GET", "/admin/api/settings", nil, "", "", cookie), 403)
	execSQL(t, f.db, "UPDATE users SET password_hash='new-hash'")
	decoded(t, request(f.handler, "GET", "/admin/api/me", nil, "", "", cookie), 401)
	cookie = f.login(t, "admin")
	execSQL(t, f.db, "UPDATE users SET status=0")
	decoded(t, request(f.handler, "GET", "/admin/api/me", nil, "", "", cookie), 401)
}

func TestProductionSettingsHealthAndSingleConnectionLists(t *testing.T) {
	f := integration(t)
	cookie := f.login(t, "admin")
	decoded(t, request(f.handler, "PUT", "/admin/api/settings", map[string]any{"publish.lease_seconds": 900, "publish.max_attempts": 1.5}, "", "", cookie), 422)
	if count(t, f.db, "SELECT CAST(setting_value AS UNSIGNED) FROM settings WHERE setting_key='publish.lease_seconds'") != 1800 {
		t.Fatal("partial settings write")
	}
	decoded(t, request(f.handler, "PUT", "/admin/api/settings", map[string]any{"publish.lease_seconds": 900, "publish.tick_interval": 600}, "", "", cookie), 200)
	f.s.lastTick.Store(time.Now().Add(-6 * time.Minute).Unix())
	decoded(t, request(f.handler, "GET", "/readyz", nil, "", "", nil), 200)
	for i := 0; i < 3; i++ {
		f.create(t, map[string]any{"title": fmt.Sprint(i)})
	}
	f.db.SetMaxOpenConns(1)
	f.db.SetMaxIdleConns(1)
	results := make(chan *httptest.ResponseRecorder, 8)
	for i := 0; i < 8; i++ {
		go func() { results <- request(f.handler, "GET", "/api/v1/articles?page=1", nil, f.token, "", nil) }()
	}
	decoded(t, request(f.handler, "GET", "/admin/api/dashboard", nil, "", "", cookie), 200)
	f.s.tick()
	second := New(f.s.cfg, f.db)
	decoded(t, request(second.Handler(), "GET", "/readyz", nil, "", "", nil), 200)
	for i := 0; i < 8; i++ {
		select {
		case w := <-results:
			decoded(t, w, 200)
		case <-time.After(5 * time.Second):
			t.Fatal("connection pool starvation")
		}
	}
	f.db.SetMaxOpenConns(24)
	f.db.SetMaxIdleConns(10)
	j := f.claim(t)
	until, err := time.Parse(time.RFC3339, j["lease_expires_at"].(string))
	if err != nil || time.Until(until) > 901*time.Second || time.Until(until) < 895*time.Second {
		t.Fatalf("lease setting not consumed: %v", j)
	}
	f.db.Close()
	decoded(t, request(f.handler, "GET", "/livez", nil, "", "", nil), 200)
	decoded(t, request(f.handler, "GET", "/readyz", nil, "", "", nil), 503)
}

func TestProductionConcurrentEditsAndSchemaGate(t *testing.T) {
	f := integration(t)
	a := f.create(t, map[string]any{"status": "draft"})
	results := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			results <- request(f.handler, "PATCH", "/api/v1/articles/"+a["uid"].(string), map[string]any{"title": fmt.Sprint("Writer ", i), "revision": a["revision"]}, f.token, "", nil)
		}(i)
	}
	codes := map[int]int{}
	for i := 0; i < 2; i++ {
		w := <-results
		codes[w.Code]++
	}
	if codes[200] != 1 || codes[409] != 1 {
		t.Fatalf("concurrent edits: %v", codes)
	}
	var checksum string
	if err := f.db.QueryRow("SELECT checksum FROM schema_migrations WHERE version='020_scheduler_heartbeat.sql'").Scan(&checksum); err != nil {
		t.Fatal(err)
	}
	execSQL(t, f.db, "DELETE FROM schema_migrations WHERE version='020_scheduler_heartbeat.sql'")
	if migrate.CheckCurrent(f.db) == nil {
		t.Fatal("missing migration accepted")
	}
	execSQL(t, f.db, "INSERT INTO schema_migrations(version,checksum) VALUES ('020_scheduler_heartbeat.sql',?)", checksum)
	execSQL(t, f.db, "INSERT INTO schema_migrations(version,checksum) VALUES ('999_future.sql',REPEAT('0',64))")
	if migrate.CheckCurrent(f.db) == nil {
		t.Fatal("newer schema accepted by old binary")
	}
}

func TestProductionFailureRequiresNoPublicationEvidence(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		t.Run(fmt.Sprint(confirmed), func(t *testing.T) {
			f := integration(t)
			f.create(t, nil)
			j := f.claim(t)
			body := map[string]any{"status": "failure", "worker_id": "worker", "lease_token": j["lease_token"], "error_code": "network_error", "not_published": confirmed}
			result := decoded(t, request(f.handler, "POST", "/api/v1/publish/"+j["uid"].(string)+"/result", body, f.token, "failure", nil), 200)["job"].(map[string]any)
			want := "uncertain"
			if confirmed {
				want = "queued"
			}
			if result["status"] != want {
				t.Fatalf("got %v want %s", result, want)
			}
		})
	}
}

func TestProductionOldFailureCannotRepublishPublishedArticle(t *testing.T) {
	f := integration(t)
	a := f.create(t, nil)
	j := f.claim(t)
	body := map[string]any{"status": "failure", "worker_id": "worker", "lease_token": j["lease_token"], "error_code": "content_rejected", "not_published": true}
	decoded(t, request(f.handler, "POST", "/api/v1/publish/"+j["uid"].(string)+"/result", body, f.token, "failure", nil), 200)
	cookie := f.login(t, "admin")
	decoded(t, request(f.handler, "POST", "/admin/api/articles/"+a["uid"].(string)+"/manual-publish", map[string]any{"platform": "wechat", "platform_account": "test", "url": "https://example.com/already-published"}, "", "", cookie), 200)
	decoded(t, request(f.handler, "POST", "/admin/api/queue/"+j["uid"].(string)+"/retry", nil, "", "", cookie), 409)
	if f.article(t, a["uid"].(string))["publish_status"] != "published" {
		t.Fatal("old failed job changed newer publication")
	}
}

func TestProductionReviewLeaseAndManualPublication(t *testing.T) {
	f := integration(t)
	a := f.create(t, map[string]any{"publish_mode": "manual"})
	admin := f.login(t, "admin")
	decoded(t, request(f.handler, "POST", "/admin/api/articles/"+a["uid"].(string)+"/manual-publish", map[string]any{"platform": "wechat", "platform_account": "test", "url": "https://example.com/manual"}, "", "", admin), 200)
	if count(t, f.db, "SELECT COUNT(*) FROM publish_jobs WHERE review_status='pending' AND content_snapshot IS NOT NULL") != 1 {
		t.Fatal("manual publication omitted review/snapshot")
	}
	execSQL(t, f.db, "UPDATE publish_jobs SET review_next_at=UTC_TIMESTAMP()")
	claims := decoded(t, request(f.handler, "POST", "/api/v1/publish/review/claim", map[string]any{"worker_id": "reviewer", "lease_protocol": 2}, f.token, "", nil), 200)["jobs"].([]any)
	if len(claims) != 1 {
		t.Fatal("missing review job")
	}
	j := claims[0].(map[string]any)
	uid := j["uid"].(string)
	decoded(t, request(f.handler, "POST", "/api/v1/publish/"+uid+"/review", map[string]any{"worker_id": "reviewer", "lease_token": "old", "status": "approved"}, f.token, "", nil), 409)
	decoded(t, request(f.handler, "POST", "/api/v1/publish/review/heartbeat", map[string]any{"worker_id": "reviewer", "uids": []string{uid}, "lease_tokens": map[string]any{uid: j["lease_token"]}}, f.token, "", nil), 200)
	decoded(t, request(f.handler, "POST", "/api/v1/publish/"+uid+"/review", map[string]any{"worker_id": "reviewer", "lease_token": j["lease_token"], "status": "approved"}, f.token, "", nil), 200)
	if f.article(t, a["uid"].(string))["review_status"] != "approved" {
		t.Fatal("review projection not updated")
	}
}

func TestProductionSchedulerLockAndCancellation(t *testing.T) {
	f := integration(t)
	var schema string
	f.db.QueryRow("SELECT DATABASE()").Scan(&schema)
	name := "contenthub.tick." + tokenDigest(schema)[:32]
	conn, err := f.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var got int
	if err = conn.QueryRowContext(context.Background(), "SELECT GET_LOCK(?,0)", name).Scan(&got); err != nil || got != 1 {
		t.Fatal("cannot set up scheduler lock")
	}
	f.s.tick()
	if f.s.lastTick.Load() != 0 {
		t.Fatal("tick ignored foreign lock")
	}
	conn.QueryRowContext(context.Background(), "SELECT RELEASE_LOCK(?)", name).Scan(&got)
	conn.Close()
	f.s.tick()
	if f.s.lastTick.Load() == 0 || count(t, f.db, "SELECT IS_FREE_LOCK(?)", name) != 1 {
		t.Fatal("scheduler did not release its lock")
	}
	h := f.s.mutate(func(s *Server, w http.ResponseWriter, r *http.Request) {
		var v int
		_ = s.db.QueryRow("SELECT SLEEP(10)").Scan(&v)
		s.writeJSON(w, 200, map[string]bool{"ok": true})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	r := httptest.NewRequest("POST", "/test", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	start := time.Now()
	h(w, r)
	if time.Since(start) > 2*time.Second || w.Code < 400 {
		t.Fatal("SQL outlived cancelled request")
	}
}
