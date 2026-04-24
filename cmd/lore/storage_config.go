package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/alferio94/lore/internal/store"
)

type StorageConfig struct {
	DataDir     string
	DatabaseURL string
	Backend     store.Backend
}

func loadStorageConfig(base store.Config) (StorageConfig, error) {
	storageCfg := StorageConfig{
		DataDir: strings.TrimSpace(base.DataDir),
		Backend: base.SelectedBackend(),
	}

	if dataDir := strings.TrimSpace(os.Getenv("LORE_DATA_DIR")); dataDir != "" {
		storageCfg.DataDir = dataDir
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL != "" {
		parsed, err := validateDatabaseURL(databaseURL)
		if err != nil {
			return StorageConfig{}, err
		}
		storageCfg.DatabaseURL = databaseURL
		if isPostgresScheme(parsed.Scheme) {
			storageCfg.Backend = store.BackendPostgreSQL
		}
	}

	return storageCfg, nil
}

func validateDatabaseURL(raw string) (*url.URL, error) {
	if !strings.Contains(raw, "://") {
		return nil, fmt.Errorf("lore config: invalid DATABASE_URL %q: expected URI scheme separator (://)", raw)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("lore config: invalid DATABASE_URL %q: %w", raw, err)
	}
	if parsed.Scheme == "" {
		return nil, fmt.Errorf("lore config: invalid DATABASE_URL %q: missing URL scheme", raw)
	}
	if parsed.Host == "" && parsed.Opaque == "" && strings.TrimSpace(parsed.Path) == "" {
		return nil, fmt.Errorf("lore config: invalid DATABASE_URL %q: missing URL authority/path", raw)
	}
	if isPostgresScheme(parsed.Scheme) {
		if parsed.Host == "" {
			return nil, fmt.Errorf("lore config: invalid DATABASE_URL %q: missing PostgreSQL host", raw)
		}
		if strings.Trim(parsed.Path, "/") == "" {
			return nil, fmt.Errorf("lore config: invalid DATABASE_URL %q: missing PostgreSQL database name", raw)
		}
	}
	return parsed, nil
}

func (cfg StorageConfig) Apply(base store.Config) store.Config {
	base.DataDir = cfg.DataDir
	base.DatabaseURL = cfg.DatabaseURL
	base.Backend = cfg.Backend
	return base
}

func isPostgresScheme(scheme string) bool {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "postgres", "postgresql":
		return true
	default:
		return false
	}
}
