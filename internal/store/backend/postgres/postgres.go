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
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_pg_obs_search_vector ON observations USING GIN ((%s))`, observationSearchVectorExpression()),
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
		`CREATE TABLE IF NOT EXISTS skills (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL,
			triggers TEXT NOT NULL DEFAULT '',
			content TEXT NOT NULL,
			compact_rules TEXT NOT NULL DEFAULT '',
			version INTEGER NOT NULL DEFAULT 1,
			is_active BOOLEAN NOT NULL DEFAULT TRUE,
			changed_by TEXT NOT NULL DEFAULT 'system',
			created_at TEXT NOT NULL DEFAULT to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS'),
			updated_at TEXT NOT NULL DEFAULT to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS')
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_skills_name ON skills(name)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_skills_active ON skills(is_active)`,
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_pg_skills_search_vector ON skills USING GIN ((%s))`, skillSearchVectorExpression()),
		`CREATE TABLE IF NOT EXISTS stacks (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_pg_stacks_name ON stacks(name)`,
		`CREATE TABLE IF NOT EXISTS categories (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_pg_categories_name ON categories(name)`,
		`CREATE TABLE IF NOT EXISTS skill_stacks (
			skill_id BIGINT NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
			stack_id BIGINT NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
			PRIMARY KEY (skill_id, stack_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_skill_stacks_stack ON skill_stacks(stack_id)`,
		`CREATE TABLE IF NOT EXISTS skill_categories (
			skill_id BIGINT NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
			category_id BIGINT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
			PRIMARY KEY (skill_id, category_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_skill_categories_category ON skill_categories(category_id)`,
		`CREATE TABLE IF NOT EXISTS skill_versions (
			id BIGSERIAL PRIMARY KEY,
			skill_id BIGINT NOT NULL REFERENCES skills(id),
			version INTEGER NOT NULL,
			content TEXT NOT NULL,
			compact_rules TEXT NOT NULL DEFAULT '',
			changed_by TEXT NOT NULL DEFAULT 'system',
			created_at TEXT NOT NULL DEFAULT to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS')
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_pg_skill_versions_skill_version_unique ON skill_versions(skill_id, version)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_skill_versions_skill ON skill_versions(skill_id, version DESC)`,
		`CREATE TABLE IF NOT EXISTS users (
			id BIGSERIAL PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL DEFAULT 'developer',
			status TEXT NOT NULL DEFAULT 'active',
			password_hash TEXT NOT NULL DEFAULT '',
			avatar_url TEXT NOT NULL DEFAULT '',
			provider TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS'),
			updated_at TEXT NOT NULL DEFAULT to_char(timezone('UTC', now()), 'YYYY-MM-DD HH24:MI:SS')
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_pg_users_email ON users(email)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_users_role ON users(role)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_users_status ON users(status)`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active'`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE users ALTER COLUMN role SET DEFAULT 'developer'`,
		`ALTER TABLE users ALTER COLUMN status SET DEFAULT 'active'`,
		`ALTER TABLE users ALTER COLUMN password_hash SET DEFAULT ''`,
		`UPDATE users SET role = 'developer' WHERE role = 'viewer'`,
		`UPDATE users SET status = 'active' WHERE status IS NULL OR status = ''`,
		`UPDATE users SET password_hash = '' WHERE password_hash IS NULL`,
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

func observationSearchVectorExpression() string {
	return strings.Join([]string{
		"setweight(to_tsvector('simple', COALESCE(title, '')), 'A')",
		"setweight(to_tsvector('simple', COALESCE(topic_key, '')), 'A')",
		"setweight(to_tsvector('simple', COALESCE(type, '')), 'B')",
		"setweight(to_tsvector('simple', COALESCE(tool_name, '')), 'B')",
		"setweight(to_tsvector('simple', COALESCE(project, '')), 'B')",
		"setweight(to_tsvector('simple', COALESCE(content, '')), 'C')",
	}, " || ")
}

func skillSearchVectorExpression() string {
	return strings.Join([]string{
		"setweight(to_tsvector('simple', COALESCE(name, '')), 'A')",
		"setweight(to_tsvector('simple', COALESCE(display_name, '')), 'A')",
		"setweight(to_tsvector('simple', COALESCE(triggers, '')), 'B')",
		"setweight(to_tsvector('simple', COALESCE(content, '')), 'C')",
	}, " || ")
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
