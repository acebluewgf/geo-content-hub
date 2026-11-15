package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"

	"contenthub/internal/config"
	"contenthub/internal/db"
	"contenthub/internal/migrate"
	"contenthub/internal/server"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "serve":
		runServe()
	case "migrate":
		runMigrate()
	case "db:check":
		runCheck()
	case "user:create":
		runUserCreate()
	case "user:passwd":
		runUserPasswd()
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `contenthub commands:
  serve            启动 HTTP 服务
  migrate          执行数据库迁移
  db:check         检查数据库版本/字符集/时区
  user:create      创建后台用户
  user:passwd      重置后台用户密码`)
	os.Exit(2)
}

func load() (*config.Config, *sql.DB) {
	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}
	database, err := db.Open(cfg.DSN)
	if err != nil {
		fatal(err)
	}
	if err := database.Ping(); err != nil {
		fatal(fmt.Errorf("db ping: %w", err))
	}
	return cfg, database
}

func runCheck() {
	_, database := load()
	if err := migrate.Check(database); err != nil {
		fatal(err)
	}
	fmt.Println("db:check ok")
}

func runMigrate() {
	_, database := load()
	if err := migrate.Check(database); err != nil {
		fatal(err)
	}
	if err := migrate.Apply(database); err != nil {
		fatal(err)
	}
	if err := migrate.Check(database); err != nil {
		fatal(err)
	}
	fmt.Println("migrate ok")
}

func runServe() {
	cfg, database := load()
	if err := migrate.Check(database); err != nil {
		fatal(err)
	}
	if err := migrate.CheckCurrent(database); err != nil {
		fatal(err)
	}
	s := server.New(cfg, database)
	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	schedulerDone := s.StartScheduler(ctx)
	if err := serveUntilStopped(ctx, httpSrv); err != nil {
		stop()
		<-schedulerDone
		database.Close()
		fatal(err)
	}
	stop()
	<-schedulerDone
	database.Close()

}

func runUserCreate() {
	fs := flag.NewFlagSet("user:create", flag.ExitOnError)
	username := fs.String("username", "", "登录名")
	role := fs.String("role", "admin", "admin/editor/viewer")
	display := fs.String("display", "", "显示名")
	password := fs.String("password", "", "密码（不填则交互输入）")
	_ = fs.Parse(os.Args[2:])
	if *username == "" {
		fatal(fmt.Errorf("--username is required"))
	}
	_, database := load()

	var exists int
	if err := database.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", *username).Scan(&exists); err != nil {
		fatal(err)
	}
	if exists > 0 {
		fatal(fmt.Errorf("user %s already exists", *username))
	}

	pw := *password
	if pw == "" {
		fmt.Print("Password: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			fatal(err)
		}
		pw = string(b)
		fmt.Print("Repeat: ")
		b2, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			fatal(err)
		}
		if pw != string(b2) {
			fatal(fmt.Errorf("passwords do not match"))
		}
	}
	if len(pw) < 10 {
		fatal(fmt.Errorf("password must be at least 10 chars"))
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	if err != nil {
		fatal(err)
	}
	if *display == "" {
		*display = *username
	}
	_, err = database.Exec(`INSERT INTO users (uid, username, password_hash, display_name, role)
		VALUES (?, ?, ?, ?, ?)`, ulid.Make().String(), *username, string(hash), *display, *role)
	if err != nil {
		fatal(err)
	}
	fmt.Println("user created:", *username)
}

func runUserPasswd() {
	fs := flag.NewFlagSet("user:passwd", flag.ExitOnError)
	username := fs.String("username", "", "登录名")
	password := fs.String("password", "", "新密码（不填则交互输入）")
	_ = fs.Parse(os.Args[2:])
	if *username == "" {
		fatal(fmt.Errorf("--username is required"))
	}
	_, database := load()

	pw := *password
	if pw == "" {
		fmt.Print("New password: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			fatal(err)
		}
		pw = string(b)
		fmt.Print("Repeat: ")
		b2, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			fatal(err)
		}
		if pw != string(b2) {
			fatal(fmt.Errorf("passwords do not match"))
		}
	}
	if len(pw) < 10 {
		fatal(fmt.Errorf("password must be at least 10 chars"))
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	if err != nil {
		fatal(err)
	}
	res, err := database.Exec(`UPDATE users SET password_hash=?, login_fail_count=0, locked_until=NULL, updated_at=UTC_TIMESTAMP()
		WHERE username=?`, string(hash), *username)
	if err != nil {
		fatal(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		fatal(fmt.Errorf("user %s not found", *username))
	}
	fmt.Println("password updated:", *username)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

// The caller stays alive until Shutdown has drained in-flight requests.
func serveUntilStopped(ctx context.Context, srv *http.Server) error {
	return serveAndDrain(ctx, srv, srv.ListenAndServe)
}
func serveAndDrain(ctx context.Context, srv *http.Server, serve func() error) error {
	served := make(chan error, 1)
	go func() { served <- serve() }()
	select {
	case err := <-served:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case <-ctx.Done():
		drain, cancel := context.WithTimeout(context.Background(), 50*time.Second)
		defer cancel()
		err := srv.Shutdown(drain)
		if err != nil {
			_ = srv.Close()
		}
		serveErr := <-served
		if err != nil {
			return err
		}
		if serveErr != nil && serveErr != http.ErrServerClosed {
			return serveErr
		}
		return nil
	}
}
