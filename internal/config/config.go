package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

type Config struct {
	Env           string
	Addr          string
	BaseURL       string
	DSN           string
	MediaDir      string
	SessionSecret string
}

func Load() (*Config, error) {
	_ = godotenv.Load(".env.local")
	_ = godotenv.Load(".env")

	cfg := &Config{
		Env:           get("APP_ENV", "development"),
		Addr:          get("APP_ADDR", "127.0.0.1:8080"),
		BaseURL:       strings.TrimRight(get("APP_BASE_URL", "http://127.0.0.1:8080"), "/"),
		MediaDir:      get("MEDIA_DIR", "storage/uploads"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
	}

	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s",
			os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"),
			get("DB_HOST", "127.0.0.1"), get("DB_PORT", "3306"), get("DB_NAME", "contenthub"))
	}
	my, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	my.Timeout = 5 * time.Second
	my.ReadTimeout = 15 * time.Second
	my.WriteTimeout = 15 * time.Second
	my.ParseTime = true
	my.Loc = time.UTC
	my.Collation = "utf8mb4_unicode_ci"
	if my.Params == nil {
		my.Params = map[string]string{}
	}
	my.Params["charset"] = "utf8mb4"
	my.Params["sql_mode"] = "'STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION,ONLY_FULL_GROUP_BY'"
	my.Params["time_zone"] = "'+00:00'"
	cfg.DSN = my.FormatDSN()

	if cfg.SessionSecret == "" {
		return nil, fmt.Errorf("SESSION_SECRET is required")
	}
	if len(cfg.SessionSecret) < 32 {
		return nil, fmt.Errorf("SESSION_SECRET must be at least 32 chars")
	}
	return cfg, nil
}

func get(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
