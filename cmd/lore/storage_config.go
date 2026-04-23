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
}

func loadStorageConfig(base store.Config) (StorageConfig, error) {
	storageCfg := StorageConfig{
		DataDir: strings.TrimSpace(base.DataDir),
	}

	if dataDir := strings.TrimSpace(os.Getenv("LORE_DATA_DIR")); dataDir != "" {
		storageCfg.DataDir = dataDir
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL != "" {
		if err := validateDatabaseURL(databaseURL); err != nil {
			return StorageConfig{}, err
		}
		storageCfg.DatabaseURL = databaseURL
	}

	return storageCfg, nil
}

func validateDatabaseURL(raw string) error {
	if !strings.Contains(raw, "://") {
		return fmt.Errorf("lore config: invalid DATABASE_URL %q: expected URI scheme separator (://)", raw)
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("lore config: invalid DATABASE_URL %q: %w", raw, err)
	}
	if parsed.Scheme == "" {
		return fmt.Errorf("lore config: invalid DATABASE_URL %q: missing URL scheme", raw)
	}
	if parsed.Host == "" && parsed.Opaque == "" && strings.TrimSpace(parsed.Path) == "" {
		return fmt.Errorf("lore config: invalid DATABASE_URL %q: missing URL authority/path", raw)
	}
	return nil
}

func (cfg StorageConfig) Apply(base store.Config) store.Config {
	base.DataDir = cfg.DataDir
	base.DatabaseURL = cfg.DatabaseURL
	return base
}
