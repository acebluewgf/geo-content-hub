package server

import (
	"context"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in fixture for a real browser; it uses the same isolated database guard
// and automatic cleanup as the integration suite, never application .env files.
func TestBrowserFixture(t *testing.T) {
	stopFile := os.Getenv("CONTENTHUB_BROWSER_STOP")
	if stopFile == "" {
		t.Skip("manual browser fixture disabled")
	}
	if !filepath.IsAbs(stopFile) {
		t.Fatal("stop marker must be an absolute path")
	}
	f := integration(t)
	f.login(t, "admin")
	var username string
	if err := f.db.QueryRow("SELECT username FROM users LIMIT 1").Scan(&username); err != nil {
		t.Fatal(err)
	}
	a := f.create(t, map[string]any{"title": "待核对测试任务"})
	j := f.claim(t)
	execSQL(t, f.db, "UPDATE publish_jobs SET lease_expires_at=DATE_SUB(UTC_TIMESTAMP(),INTERVAL 1 SECOND) WHERE uid=?", j["uid"])
	f.s.tick()
	_ = a
	listener, err := net.Listen("tcp", "127.0.0.1:18087")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(f.handler)
	srv.Listener = listener
	f.s.cfg.BaseURL = "http://127.0.0.1:18087"
	f.s.media.BaseURL = f.s.cfg.BaseURL
	srv.Start()
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := f.s.StartScheduler(ctx)
	defer func() { cancel(); <-done }()
	t.Logf("BROWSER_READY url=%s/admin/ username=%s password=test-password-123", srv.URL, username)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	timeout := time.NewTimer(20 * time.Minute)
	defer timeout.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err := os.Stat(stopFile); err == nil {
				return
			}
		case <-timeout.C:
			t.Fatal("browser fixture expired")
		}
	}
}
