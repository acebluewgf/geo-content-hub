package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"contenthub/migrations"
)

func Check(d *sql.DB) error {
	var version string
	if err := d.QueryRow("SELECT VERSION()").Scan(&version); err != nil {
		return fmt.Errorf("select version: %w", err)
	}
	base := strings.SplitN(version, "-", 2)[0]
	var major, minor, patch int
	if _, err := fmt.Sscanf(base, "%d.%d.%d", &major, &minor, &patch); err != nil {
		return fmt.Errorf("unexpected mysql version %q", version)
	}
	ok := (major == 5 && minor == 7 && patch >= 30) || (major == 8 && minor == 0)
	if !ok {
		return fmt.Errorf("mysql %s unsupported: need 5.7.30+ or 8.0.x", version)
	}

	var mode, tz string
	if err := d.QueryRow("SELECT @@SESSION.sql_mode, @@SESSION.time_zone").Scan(&mode, &tz); err != nil {
		return fmt.Errorf("select session vars: %w", err)
	}
	if !strings.Contains(mode, "ONLY_FULL_GROUP_BY") {
		return fmt.Errorf("sql_mode missing ONLY_FULL_GROUP_BY: %s", mode)
	}
	if tz != "+00:00" {
		return fmt.Errorf("session time_zone is %q, want +00:00", tz)
	}

	var schema sql.NullString
	if err := d.QueryRow("SELECT DATABASE()").Scan(&schema); err != nil || !schema.Valid || schema.String == "" {
		return fmt.Errorf("no database selected")
	}
	rows, err := d.Query(`SELECT TABLE_NAME, ROW_FORMAT, TABLE_COLLATION
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'`, schema.String)
	if err != nil {
		return fmt.Errorf("inspect tables: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, rowFormat, collation string
		if err := rows.Scan(&name, &rowFormat, &collation); err != nil {
			return err
		}
		if rowFormat != "Dynamic" {
			return fmt.Errorf("table %s row_format=%s, want Dynamic", name, rowFormat)
		}
		if strings.HasPrefix(collation, "utf8mb4_0900") {
			return fmt.Errorf("table %s uses 8.0-only collation %s", name, collation)
		}
		if collation != "utf8mb4_unicode_ci" {
			return fmt.Errorf("table %s collation=%s, want utf8mb4_unicode_ci", name, collation)
		}
	}
	return rows.Err()
}

func Apply(d *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	conn, err := d.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var locked int
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(CONCAT('ch:migrate:',LEFT(SHA2(DATABASE(),256),40)),30)").Scan(&locked); err != nil {
		return err
	}
	if locked != 1 {
		return fmt.Errorf("another migration is running")
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(release, "DO RELEASE_LOCK(CONCAT('ch:migrate:',LEFT(SHA2(DATABASE(),256),40)))")
	}()
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(64) NOT NULL,
		checksum CHAR(64) NOT NULL,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (version)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci ROW_FORMAT=DYNAMIC`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	// widen for existing installs created with VARCHAR(20)
	if _, err := conn.ExecContext(ctx, "ALTER TABLE schema_migrations MODIFY version VARCHAR(64) NOT NULL"); err != nil {
		return err
	}

	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, name := range files {
		raw, err := migrations.FS.ReadFile(name)
		if err != nil {
			return err
		}
		sum := fmt.Sprintf("%x", sha256.Sum256(raw))
		var applied string
		err = conn.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version = ?", name).Scan(&applied)
		switch {
		case err == nil:
			if applied != sum {
				return fmt.Errorf("migration %s checksum mismatch (db=%s file=%s)", name, applied, sum)
			}
			continue
		case err != sql.ErrNoRows:
			return err
		}
		for _, stmt := range strings.Split(string(raw), ";") {
			s := strings.TrimSpace(stmt)
			if s == "" {
				continue
			}
			if _, err := conn.ExecContext(ctx, s); err != nil {
				return fmt.Errorf("migration %s: %w", name, err)
			}
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations (version, checksum) VALUES (?, ?)", name, sum); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		fmt.Println("applied", name)
	}
	return nil
}

// Refuse to serve with missing, modified or newer schema migrations.
func CheckCurrent(d *sql.DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rows, err := d.QueryContext(ctx, "SELECT version,checksum FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("schema not ready; run migrate: %w", err)
	}
	applied := map[string]string{}
	for rows.Next() {
		var name, sum string
		if err := rows.Scan(&name, &sum); err != nil {
			rows.Close()
			return err
		}
		applied[name] = sum
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		raw, err := migrations.FS.ReadFile(entry.Name())
		if err != nil {
			return err
		}
		if applied[entry.Name()] != fmt.Sprintf("%x", sha256.Sum256(raw)) {
			return fmt.Errorf("schema migration %s missing or changed; run migrate before serve", entry.Name())
		}
		delete(applied, entry.Name())
	}
	if len(applied) > 0 {
		return fmt.Errorf("database schema is newer than this binary")
	}
	return nil
}
