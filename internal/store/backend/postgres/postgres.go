package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

var pingDatabase = func(ctx context.Context, db *sql.DB) error {
	return db.PingContext(ctx)
}

func OpenDatabase(raw string, open func(driverName, dataSourceName string) (*sql.DB, error)) (*sql.DB, error) {
	normalized, err := NormalizeDSN(raw)
	if err != nil {
		return nil, err
	}

	db, err := open("postgres", normalized)
	if err != nil {
		return nil, fmt.Errorf("lore: open postgres database: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pingDatabase(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("lore: ping postgres database: %w", err)
	}

	if err := Bootstrap(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func NormalizeDSN(raw string) (string, error) {
	if !strings.Contains(raw, "://") {
		return "", fmt.Errorf("lore: invalid postgres DATABASE_URL %q: expected URI scheme separator (://)", raw)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("lore: invalid postgres DATABASE_URL %q: %w", raw, err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return "", fmt.Errorf("lore: invalid postgres DATABASE_URL %q: unsupported scheme %q", raw, parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("lore: invalid postgres DATABASE_URL %q: missing host", raw)
	}
	if strings.Trim(parsed.Path, "/") == "" {
		return "", fmt.Errorf("lore: invalid postgres DATABASE_URL %q: missing database name", raw)
	}

	query := parsed.Query()
	if isLocalHost(parsed.Hostname()) && query.Get("sslmode") == "" {
		query.Set("sslmode", "disable")
		parsed.RawQuery = query.Encode()
	}

	return parsed.String(), nil
}

func Bootstrap(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			project TEXT NOT NULL,
			directory TEXT NOT NULL,
			started_at TEXT NOT NULL DEFAULT to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS'),
			ended_at TEXT,
			summary TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS observations (
			id BIGSERIAL PRIMARY KEY,
			sync_id TEXT,
			session_id TEXT NOT NULL REFERENCES sessions(id),
			type TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			tool_name TEXT,
			project TEXT,
			scope TEXT NOT NULL DEFAULT 'project',
			topic_key TEXT,
			normalized_hash TEXT,
			revision_count INTEGER NOT NULL DEFAULT 1,
			duplicate_count INTEGER NOT NULL DEFAULT 1,
			last_seen_at TEXT,
			created_at TEXT NOT NULL DEFAULT to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS'),
			updated_at TEXT NOT NULL DEFAULT to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS'),
			deleted_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_obs_session ON observations(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_obs_type ON observations(type)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_obs_project ON observations(project)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_obs_created ON observations(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_obs_topic_project_scope ON observations(topic_key, project, scope)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_obs_hash_project_scope ON observations(normalized_hash, project, scope)`,
		`CREATE TABLE IF NOT EXISTS sync_state (
			target_key TEXT PRIMARY KEY,
			lifecycle TEXT NOT NULL DEFAULT 'idle',
			last_enqueued_seq BIGINT NOT NULL DEFAULT 0,
			last_acked_seq BIGINT NOT NULL DEFAULT 0,
			last_pulled_seq BIGINT NOT NULL DEFAULT 0,
			consecutive_failures INTEGER NOT NULL DEFAULT 0,
			backoff_until TEXT,
			lease_owner TEXT,
			lease_until TEXT,
			last_error TEXT,
			updated_at TEXT NOT NULL DEFAULT to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS')
		)`,
		`CREATE TABLE IF NOT EXISTS sync_mutations (
			seq BIGSERIAL PRIMARY KEY,
			target_key TEXT NOT NULL REFERENCES sync_state(target_key),
			entity TEXT NOT NULL,
			entity_key TEXT NOT NULL,
			op TEXT NOT NULL,
			payload TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'local',
			project TEXT NOT NULL DEFAULT '',
			occurred_at TEXT NOT NULL DEFAULT to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS'),
			acked_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_sync_mutations_target_seq ON sync_mutations(target_key, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_sync_mutations_project ON sync_mutations(project)`,
	}

	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return fmt.Errorf("lore: bootstrap postgres schema: %w", err)
		}
	}

	if _, err := db.Exec(`
		INSERT INTO sync_state (target_key, lifecycle, updated_at)
		VALUES ($1, $2, to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS'))
		ON CONFLICT (target_key) DO NOTHING
	`, "cloud", "idle"); err != nil {
		return fmt.Errorf("lore: bootstrap postgres sync_state seed: %w", err)
	}

	return nil
}

func isLocalHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
