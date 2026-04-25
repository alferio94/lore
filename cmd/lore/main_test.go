package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore/internal/admin"
	"github.com/alferio94/lore/internal/mcp"
	"github.com/alferio94/lore/internal/obsidian"
	"github.com/alferio94/lore/internal/server"
	"github.com/alferio94/lore/internal/store"
	versioncheck "github.com/alferio94/lore/internal/version"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func testConfig(t *testing.T) store.Config {
	t.Helper()
	cfg, err := store.DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig: %v", err)
	}
	cfg.DataDir = t.TempDir()
	return cfg
}

func withArgs(t *testing.T, args ...string) {
	t.Helper()
	old := os.Args
	os.Args = args
	t.Cleanup(func() {
		os.Args = old
	})
}

func withCwd(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to %s: %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(old)
	})
}

type noopStore struct{ store.Contract }

func (noopStore) Close() error { return nil }

func (noopStore) BootstrapAdmin(string, string, string) (*store.User, error) { return nil, nil }

type bootstrapRecorderStore struct {
	noopStore
	called       bool
	email        string
	name         string
	passwordHash string
}

func (s *bootstrapRecorderStore) BootstrapAdmin(email, name, passwordHash string) (*store.User, error) {
	s.called = true
	s.email = email
	s.name = name
	s.passwordHash = passwordHash
	return &store.User{Email: email, Name: name, Role: store.UserRoleAdmin, Status: store.UserStatusActive}, nil
}

func stubCheckForUpdates(t *testing.T, result versioncheck.CheckResult) {
	t.Helper()
	old := checkForUpdates
	checkForUpdates = func(string) versioncheck.CheckResult { return result }
	t.Cleanup(func() { checkForUpdates = old })
}

func captureOutput(t *testing.T, fn func()) (stdout string, stderr string) {
	t.Helper()

	oldOut := os.Stdout
	oldErr := os.Stderr

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	os.Stdout = outW
	os.Stderr = errW

	fn()

	_ = outW.Close()
	_ = errW.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr

	outBytes, err := io.ReadAll(outR)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	errBytes, err := io.ReadAll(errR)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}

	return string(outBytes), string(errBytes)
}

func mustSeedObservation(t *testing.T, cfg store.Config, sessionID, project, typ, title, content, scope string) int64 {
	t.Helper()

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer s.Close()

	if err := s.CreateSession(sessionID, project, "/tmp"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	id, err := s.AddObservation(store.AddObservationParams{
		SessionID: sessionID,
		Type:      typ,
		Title:     title,
		Content:   content,
		Project:   project,
		Scope:     scope,
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	return id
}

func TestLoadRuntimeConfigLocalDefaults(t *testing.T) {
	t.Setenv("LORE_ENV", "")
	t.Setenv("LORE_HOST", "")
	t.Setenv("LORE_PORT", "")
	t.Setenv("PORT", "")
	t.Setenv("LORE_BASE_URL", "")
	t.Setenv("LORE_JWT_SECRET", "")
	t.Setenv("LORE_COOKIE_SECURE", "")
	t.Setenv("LORE_GOOGLE_CLIENT_ID", "")
	t.Setenv("LORE_GOOGLE_CLIENT_SECRET", "")
	t.Setenv("LORE_GITHUB_CLIENT_ID", "")
	t.Setenv("LORE_GITHUB_CLIENT_SECRET", "")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_EMAIL", "")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_PASSWORD", "")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_NAME", "")

	runtimeCfg, err := loadRuntimeConfig([]string{})
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v", err)
	}

	if runtimeCfg.Env != RuntimeEnvLocal {
		t.Fatalf("Env = %q, want %q", runtimeCfg.Env, RuntimeEnvLocal)
	}
	if runtimeCfg.Host != "127.0.0.1" {
		t.Fatalf("Host = %q, want 127.0.0.1", runtimeCfg.Host)
	}
	if runtimeCfg.Port != 7437 {
		t.Fatalf("Port = %d, want 7437", runtimeCfg.Port)
	}
	if runtimeCfg.BaseURL != "http://127.0.0.1:7437" {
		t.Fatalf("BaseURL = %q, want http://127.0.0.1:7437", runtimeCfg.BaseURL)
	}
	if runtimeCfg.CookieSecure {
		t.Fatalf("CookieSecure = true, want false")
	}
	if runtimeCfg.BootstrapAdminEmail != "admin@admin.com" {
		t.Fatalf("BootstrapAdminEmail = %q, want admin@admin.com", runtimeCfg.BootstrapAdminEmail)
	}
	if runtimeCfg.BootstrapAdminPassword != "" {
		t.Fatalf("BootstrapAdminPassword = %q, want empty", runtimeCfg.BootstrapAdminPassword)
	}
}

func TestLoadRuntimeConfigStagingGuardrails(t *testing.T) {
	t.Setenv("LORE_ENV", "staging")
	t.Setenv("LORE_HOST", "")
	t.Setenv("LORE_PORT", "")
	t.Setenv("PORT", "")
	t.Setenv("LORE_COOKIE_SECURE", "")
	t.Setenv("LORE_GOOGLE_CLIENT_ID", "")
	t.Setenv("LORE_GOOGLE_CLIENT_SECRET", "")
	t.Setenv("LORE_GITHUB_CLIENT_ID", "")
	t.Setenv("LORE_GITHUB_CLIENT_SECRET", "")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_EMAIL", "")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_NAME", "")

	t.Run("missing LORE_BASE_URL", func(t *testing.T) {
		t.Setenv("LORE_BASE_URL", "")
		t.Setenv("LORE_JWT_SECRET", "staging-secret-at-least-32-bytes")
		t.Setenv("LORE_BOOTSTRAP_ADMIN_PASSWORD", "bootstrap-secret")

		_, err := loadRuntimeConfig(nil)
		if err == nil || !strings.Contains(err.Error(), "LORE_BASE_URL is required when LORE_ENV=staging") {
			t.Fatalf("expected LORE_BASE_URL staging error, got %v", err)
		}
	})

	t.Run("missing LORE_JWT_SECRET", func(t *testing.T) {
		t.Setenv("LORE_BASE_URL", "https://staging.lore.local")
		t.Setenv("LORE_JWT_SECRET", "")
		t.Setenv("LORE_BOOTSTRAP_ADMIN_PASSWORD", "bootstrap-secret")

		_, err := loadRuntimeConfig(nil)
		if err == nil || !strings.Contains(err.Error(), "LORE_JWT_SECRET is required when LORE_ENV=staging") {
			t.Fatalf("expected LORE_JWT_SECRET staging error, got %v", err)
		}
	})

	t.Run("short LORE_JWT_SECRET", func(t *testing.T) {
		t.Setenv("LORE_BASE_URL", "https://staging.lore.local")
		t.Setenv("LORE_JWT_SECRET", "short-secret")
		t.Setenv("LORE_BOOTSTRAP_ADMIN_PASSWORD", "bootstrap-secret")

		_, err := loadRuntimeConfig(nil)
		if err == nil || !strings.Contains(err.Error(), "LORE_JWT_SECRET must be at least 32 bytes") {
			t.Fatalf("expected LORE_JWT_SECRET length staging error, got %v", err)
		}
	})

	t.Run("missing LORE_BOOTSTRAP_ADMIN_PASSWORD", func(t *testing.T) {
		t.Setenv("LORE_BASE_URL", "https://staging.lore.local")
		t.Setenv("LORE_JWT_SECRET", "staging-secret-at-least-32-bytes")
		t.Setenv("LORE_BOOTSTRAP_ADMIN_PASSWORD", "")

		_, err := loadRuntimeConfig(nil)
		if err == nil || !strings.Contains(err.Error(), "LORE_BOOTSTRAP_ADMIN_PASSWORD is required when LORE_ENV=staging") {
			t.Fatalf("expected bootstrap password staging error, got %v", err)
		}
	})

	t.Run("staging defaults", func(t *testing.T) {
		t.Setenv("LORE_BASE_URL", "https://staging.lore.local")
		t.Setenv("LORE_JWT_SECRET", "staging-secret-at-least-32-bytes")
		t.Setenv("LORE_BOOTSTRAP_ADMIN_EMAIL", "admin@staging.lore.local")
		t.Setenv("LORE_BOOTSTRAP_ADMIN_PASSWORD", "bootstrap-secret")

		runtimeCfg, err := loadRuntimeConfig(nil)
		if err != nil {
			t.Fatalf("loadRuntimeConfig() error = %v", err)
		}
		if runtimeCfg.Env != RuntimeEnvStaging {
			t.Fatalf("Env = %q, want %q", runtimeCfg.Env, RuntimeEnvStaging)
		}
		if runtimeCfg.Host != "0.0.0.0" {
			t.Fatalf("Host = %q, want 0.0.0.0", runtimeCfg.Host)
		}
		if !runtimeCfg.CookieSecure {
			t.Fatalf("CookieSecure = false, want true")
		}
		if runtimeCfg.BootstrapAdminEmail != "admin@staging.lore.local" {
			t.Fatalf("BootstrapAdminEmail = %q, want admin@staging.lore.local", runtimeCfg.BootstrapAdminEmail)
		}
	})

	t.Run("missing LORE_BOOTSTRAP_ADMIN_EMAIL", func(t *testing.T) {
		t.Setenv("LORE_BASE_URL", "https://staging.lore.local")
		t.Setenv("LORE_JWT_SECRET", "staging-secret-at-least-32-bytes")
		t.Setenv("LORE_BOOTSTRAP_ADMIN_EMAIL", "")
		t.Setenv("LORE_BOOTSTRAP_ADMIN_PASSWORD", "bootstrap-secret")

		_, err := loadRuntimeConfig(nil)
		if err == nil || !strings.Contains(err.Error(), "LORE_BOOTSTRAP_ADMIN_EMAIL is required when LORE_ENV=staging") {
			t.Fatalf("expected bootstrap email staging error, got %v", err)
		}
	})
}

func TestLoadRuntimeConfigRailwayStagingUsesPublicURLAndPortFallback(t *testing.T) {
	t.Setenv("LORE_ENV", "staging")
	t.Setenv("LORE_HOST", "")
	t.Setenv("LORE_PORT", "")
	t.Setenv("PORT", "8443")
	t.Setenv("LORE_BASE_URL", "https://preview.up.railway.app")
	t.Setenv("LORE_JWT_SECRET", "railway-secret-at-least-32-bytes")
	t.Setenv("LORE_COOKIE_SECURE", "")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_EMAIL", "admin@railway.local")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_PASSWORD", "bootstrap-secret")

	runtimeCfg, err := loadRuntimeConfig(nil)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v", err)
	}

	if runtimeCfg.Host != "0.0.0.0" {
		t.Fatalf("Host = %q, want 0.0.0.0", runtimeCfg.Host)
	}
	if runtimeCfg.Port != 8443 {
		t.Fatalf("Port = %d, want 8443 from PORT", runtimeCfg.Port)
	}
	if runtimeCfg.BaseURL != "https://preview.up.railway.app" {
		t.Fatalf("BaseURL = %q, want https://preview.up.railway.app", runtimeCfg.BaseURL)
	}
	if runtimeCfg.JWTSecret != "railway-secret-at-least-32-bytes" {
		t.Fatalf("JWTSecret = %q, want railway-secret-at-least-32-bytes", runtimeCfg.JWTSecret)
	}
	if !runtimeCfg.CookieSecure {
		t.Fatalf("CookieSecure = false, want true")
	}
}

func TestLoadRuntimeConfigUsesExplicitBootstrapAdminValues(t *testing.T) {
	t.Setenv("LORE_ENV", "local")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_EMAIL", "owner@example.com")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_PASSWORD", "secret-password")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_NAME", "Owner")

	runtimeCfg, err := loadRuntimeConfig(nil)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v", err)
	}
	if runtimeCfg.BootstrapAdminEmail != "owner@example.com" {
		t.Fatalf("BootstrapAdminEmail = %q, want owner@example.com", runtimeCfg.BootstrapAdminEmail)
	}
	if runtimeCfg.BootstrapAdminPassword != "secret-password" {
		t.Fatalf("BootstrapAdminPassword = %q, want secret-password", runtimeCfg.BootstrapAdminPassword)
	}
	if runtimeCfg.BootstrapAdminName != "Owner" {
		t.Fatalf("BootstrapAdminName = %q, want Owner", runtimeCfg.BootstrapAdminName)
	}
}

func TestLoadRuntimeConfigInvalidValues(t *testing.T) {
	t.Setenv("LORE_ENV", "prod")
	if _, err := loadRuntimeConfig(nil); err == nil || !strings.Contains(err.Error(), "invalid LORE_ENV") {
		t.Fatalf("expected invalid env error, got %v", err)
	}

	t.Setenv("LORE_ENV", "local")
	t.Setenv("LORE_COOKIE_SECURE", "definitely")
	if _, err := loadRuntimeConfig(nil); err == nil || !strings.Contains(err.Error(), "invalid LORE_COOKIE_SECURE") {
		t.Fatalf("expected invalid cookie bool error, got %v", err)
	}
}

func TestAdminGenerateSecretProducesHexSecret(t *testing.T) {
	secret := adminGenerateSecret()
	if len(secret) != 32 {
		t.Fatalf("secret length = %d, want 32 hex chars", len(secret))
	}
	for _, b := range secret {
		if !((b >= '0' && b <= '9') || (b >= 'a' && b <= 'f')) {
			t.Fatalf("secret contains non-hex byte %q", b)
		}
	}
}

func TestResolveHomeFallback(t *testing.T) {
	t.Run("uses USERPROFILE when present", func(t *testing.T) {
		t.Setenv("USERPROFILE", "/tmp/userprofile")
		t.Setenv("HOME", "")
		t.Setenv("LOCALAPPDATA", "")
		if got := resolveHomeFallback(); got != "/tmp/userprofile" {
			t.Fatalf("resolveHomeFallback() = %q, want /tmp/userprofile", got)
		}
	})

	t.Run("derives parent from LOCALAPPDATA", func(t *testing.T) {
		t.Setenv("USERPROFILE", "")
		t.Setenv("HOME", "")
		t.Setenv("LOCALAPPDATA", "/Users/alice/AppData/Local")
		if got := resolveHomeFallback(); got != "/Users/alice" {
			t.Fatalf("resolveHomeFallback() = %q, want /Users/alice", got)
		}
	})

	t.Run("returns empty when no hints exist", func(t *testing.T) {
		t.Setenv("USERPROFILE", "")
		t.Setenv("HOME", "")
		t.Setenv("LOCALAPPDATA", "")
		if got := resolveHomeFallback(); got != "" {
			t.Fatalf("resolveHomeFallback() = %q, want empty", got)
		}
	})
}

func TestAdminBuildOAuthConfigsWiresCallbacks(t *testing.T) {
	base := admin.AdminConfig{
		BaseURL:            "https://lore.example",
		GoogleClientID:     "google-id",
		GoogleClientSecret: "google-secret",
		GitHubClientID:     "github-id",
		GitHubClientSecret: "github-secret",
	}

	built := adminBuildOAuthConfigs(base)
	if built.GoogleOAuth == nil {
		t.Fatalf("expected GoogleOAuth config when credentials are set")
	}
	if built.GoogleOAuth.RedirectURL != "https://lore.example/admin/auth/callback/google" {
		t.Fatalf("google redirect = %q", built.GoogleOAuth.RedirectURL)
	}
	if built.GithubOAuth == nil {
		t.Fatalf("expected GithubOAuth config when credentials are set")
	}
	if built.GithubOAuth.RedirectURL != "https://lore.example/admin/auth/callback/github" {
		t.Fatalf("github redirect = %q", built.GithubOAuth.RedirectURL)
	}

	withoutCreds := adminBuildOAuthConfigs(admin.AdminConfig{BaseURL: "https://lore.example"})
	if withoutCreds.GoogleOAuth != nil || withoutCreds.GithubOAuth != nil {
		t.Fatalf("expected oauth configs to stay nil without credentials")
	}
}

func TestLoadStorageConfigDefaultsToStoreConfig(t *testing.T) {
	base := testConfig(t)
	t.Setenv("LORE_ENV", "")
	t.Setenv("LORE_DATA_DIR", "")
	t.Setenv("DATABASE_URL", "")

	storageCfg, err := loadStorageConfig(base)
	if err != nil {
		t.Fatalf("loadStorageConfig() error = %v", err)
	}

	if storageCfg.DataDir != base.DataDir {
		t.Fatalf("DataDir = %q, want %q", storageCfg.DataDir, base.DataDir)
	}
	if storageCfg.DatabaseURL != "" {
		t.Fatalf("DatabaseURL = %q, want empty", storageCfg.DatabaseURL)
	}
	if storageCfg.Backend != store.BackendSQLite {
		t.Fatalf("Backend = %q, want %q", storageCfg.Backend, store.BackendSQLite)
	}
}

func TestLoadStorageConfigReadsOverridesAndValidatesDatabaseURL(t *testing.T) {
	base := testConfig(t)
	overrideDir := t.TempDir()

	t.Setenv("LORE_ENV", "")
	t.Setenv("LORE_DATA_DIR", overrideDir)
	t.Setenv("DATABASE_URL", "postgres://lore:secret@db.internal:5432/lore")

	storageCfg, err := loadStorageConfig(base)
	if err != nil {
		t.Fatalf("loadStorageConfig() error = %v", err)
	}

	if storageCfg.DataDir != overrideDir {
		t.Fatalf("DataDir = %q, want %q", storageCfg.DataDir, overrideDir)
	}
	if storageCfg.DatabaseURL != "postgres://lore:secret@db.internal:5432/lore" {
		t.Fatalf("DatabaseURL = %q, want original URL", storageCfg.DatabaseURL)
	}
	if storageCfg.Backend != store.BackendPostgreSQL {
		t.Fatalf("Backend = %q, want %q", storageCfg.Backend, store.BackendPostgreSQL)
	}

	applied := storageCfg.Apply(base)
	if applied.DataDir != overrideDir {
		t.Fatalf("applied DataDir = %q, want %q", applied.DataDir, overrideDir)
	}
	if applied.DatabaseURL != storageCfg.DatabaseURL {
		t.Fatalf("applied DatabaseURL = %q, want %q", applied.DatabaseURL, storageCfg.DatabaseURL)
	}
	if applied.SelectedBackend() != store.BackendPostgreSQL {
		t.Fatalf("applied SelectedBackend() = %q, want %q", applied.SelectedBackend(), store.BackendPostgreSQL)
	}
}

func TestLoadStorageConfigAcceptsPathBasedDatabaseURL(t *testing.T) {
	base := testConfig(t)
	t.Setenv("LORE_ENV", "")
	t.Setenv("DATABASE_URL", "sqlite:///tmp/lore.db")

	storageCfg, err := loadStorageConfig(base)
	if err != nil {
		t.Fatalf("loadStorageConfig() error = %v", err)
	}
	if storageCfg.DatabaseURL != "sqlite:///tmp/lore.db" {
		t.Fatalf("DatabaseURL = %q, want sqlite:///tmp/lore.db", storageCfg.DatabaseURL)
	}
	if storageCfg.Backend != store.BackendSQLite {
		t.Fatalf("Backend = %q, want %q", storageCfg.Backend, store.BackendSQLite)
	}
}

func TestLoadStorageConfigRejectsMissingDatabaseURLInStaging(t *testing.T) {
	base := testConfig(t)
	t.Setenv("LORE_ENV", "staging")
	t.Setenv("DATABASE_URL", "")

	_, err := loadStorageConfig(base)
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL is required") {
		t.Fatalf("expected staging DATABASE_URL requirement error, got %v", err)
	}
}

func TestLoadStorageConfigRejectsNonPostgresDatabaseURLInStaging(t *testing.T) {
	base := testConfig(t)
	t.Setenv("LORE_ENV", "staging")
	t.Setenv("DATABASE_URL", "sqlite:///tmp/lore.db")

	_, err := loadStorageConfig(base)
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL must use postgres:// or postgresql://") {
		t.Fatalf("expected staging PostgreSQL DATABASE_URL error, got %v", err)
	}
}

func TestLoadStorageConfigRejectsMalformedDatabaseURL(t *testing.T) {
	base := testConfig(t)
	t.Setenv("LORE_ENV", "")
	t.Setenv("DATABASE_URL", "localhost:5432/lore")

	_, err := loadStorageConfig(base)
	if err == nil {
		t.Fatalf("expected DATABASE_URL validation error")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL mention in error, got %v", err)
	}
}

func TestCmdServeStagingMissingJWTSecretFailsFast(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	t.Setenv("LORE_ENV", "staging")
	t.Setenv("LORE_BASE_URL", "https://staging.lore.local")
	t.Setenv("LORE_JWT_SECRET", "")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_PASSWORD", "bootstrap-secret")
	t.Setenv("DATABASE_URL", "postgres://lore:secret@db.internal:5432/lore")
	withArgs(t, "lore", "serve")

	storeCalled := false
	storeOpen = func(store.Config) (store.Contract, error) {
		storeCalled = true
		return nil, nil
	}

	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdServe(cfg) })

	if _, ok := recovered.(exitCode); !ok {
		t.Fatalf("expected fatal exit, got %v", recovered)
	}
	if !strings.Contains(stderr, "LORE_JWT_SECRET is required when LORE_ENV=staging") {
		t.Fatalf("expected staging JWT guardrail error, got %q", stderr)
	}
	if storeCalled {
		t.Fatalf("expected fail-fast before store initialization")
	}
}

func TestCmdServeInvalidDatabaseURLFailsFastBeforeStoreInit(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	t.Setenv("LORE_ENV", "local")
	t.Setenv("DATABASE_URL", "postgres://")
	withArgs(t, "lore", "serve")

	storeCalled := false
	storeOpen = func(store.Config) (store.Contract, error) {
		storeCalled = true
		return nil, nil
	}

	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdServe(cfg) })

	if _, ok := recovered.(exitCode); !ok {
		t.Fatalf("expected fatal exit, got %v", recovered)
	}
	if !strings.Contains(stderr, "DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL config error, got %q", stderr)
	}
	if storeCalled {
		t.Fatalf("expected fail-fast before store initialization")
	}
}

func TestCmdServeStagingMissingDatabaseURLFailsFastBeforeStoreInit(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	t.Setenv("LORE_ENV", "staging")
	t.Setenv("LORE_BASE_URL", "https://staging.lore.local")
	t.Setenv("LORE_JWT_SECRET", "staging-secret-at-least-32-bytes")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_EMAIL", "admin@staging.lore.local")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_PASSWORD", "bootstrap-secret")
	t.Setenv("DATABASE_URL", "")
	withArgs(t, "lore", "serve")

	storeCalled := false
	storeOpen = func(store.Config) (store.Contract, error) {
		storeCalled = true
		return nil, nil
	}

	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdServe(cfg) })
	if _, ok := recovered.(exitCode); !ok {
		t.Fatalf("expected fatal exit, got %v", recovered)
	}
	if !strings.Contains(stderr, "DATABASE_URL is required") {
		t.Fatalf("expected staging DATABASE_URL requirement error, got %q", stderr)
	}
	if storeCalled {
		t.Fatalf("expected fail-fast before store initialization")
	}
}

func TestCmdServeStagingNonPostgresDatabaseURLFailsFastBeforeStoreInit(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	t.Setenv("LORE_ENV", "staging")
	t.Setenv("LORE_BASE_URL", "https://staging.lore.local")
	t.Setenv("LORE_JWT_SECRET", "staging-secret-at-least-32-bytes")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_EMAIL", "admin@staging.lore.local")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_PASSWORD", "bootstrap-secret")
	t.Setenv("DATABASE_URL", "sqlite:///tmp/lore.db")
	withArgs(t, "lore", "serve")

	storeCalled := false
	storeOpen = func(store.Config) (store.Contract, error) {
		storeCalled = true
		return nil, nil
	}

	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdServe(cfg) })
	if _, ok := recovered.(exitCode); !ok {
		t.Fatalf("expected fatal exit, got %v", recovered)
	}
	if !strings.Contains(stderr, "DATABASE_URL must use postgres:// or postgresql://") {
		t.Fatalf("expected staging PostgreSQL DATABASE_URL error, got %q", stderr)
	}
	if storeCalled {
		t.Fatalf("expected fail-fast before store initialization")
	}
}

func TestCmdServeValidPostgresDatabaseURLEnablesPostgresPath(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	storageDir := t.TempDir()

	t.Setenv("LORE_ENV", "local")
	t.Setenv("LORE_HOST", "127.0.0.1")
	t.Setenv("PORT", "7555")
	t.Setenv("LORE_DATA_DIR", storageDir)
	t.Setenv("DATABASE_URL", "postgres://lore:secret@db.internal:5432/lore")
	withArgs(t, "lore", "serve")

	seenDatabaseURL := ""
	seenDataDir := ""
	seenPort := 0
	storeOpen = func(in store.Config) (store.Contract, error) {
		seenDatabaseURL = in.DatabaseURL
		seenDataDir = in.DataDir
		if in.SelectedBackend() != store.BackendPostgreSQL {
			t.Fatalf("SelectedBackend() = %q, want %q", in.SelectedBackend(), store.BackendPostgreSQL)
		}
		return noopStore{}, nil
	}
	newHTTPServer = func(s store.Contract, cfg server.Config) *server.Server {
		seenPort = cfg.Port
		return server.NewWithConfig(s, cfg)
	}
	startHTTP = func(_ *server.Server) error { return nil }

	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdServe(cfg) })
	if recovered != nil {
		t.Fatalf("expected successful startup, got panic=%v stderr=%q", recovered, stderr)
	}
	if seenDatabaseURL == "" {
		t.Fatalf("expected store config to retain DATABASE_URL")
	}
	if seenDataDir != storageDir {
		t.Fatalf("expected storage config DataDir %q, got %q", storageDir, seenDataDir)
	}
	if seenPort != 7555 {
		t.Fatalf("expected runtime port 7555 from PORT fallback, got %d", seenPort)
	}
}

func TestCmdMCPUsesDatabaseURLStorageConfig(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	t.Setenv("DATABASE_URL", "postgres://lore:secret@db.internal:5432/lore")
	withArgs(t, "lore", "mcp")

	seenDatabaseURL := ""
	storeOpen = func(in store.Config) (store.Contract, error) {
		seenDatabaseURL = in.DatabaseURL
		if in.SelectedBackend() != store.BackendPostgreSQL {
			t.Fatalf("SelectedBackend() = %q, want %q", in.SelectedBackend(), store.BackendPostgreSQL)
		}
		return noopStore{}, nil
	}

	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdMCP(cfg) })
	if recovered != nil {
		t.Fatalf("expected successful mcp startup, got panic=%v stderr=%q", recovered, stderr)
	}
	if seenDatabaseURL == "" {
		t.Fatalf("expected store config to retain DATABASE_URL")
	}
}

func TestCmdMCPInvalidDatabaseURLFailsFastBeforeStoreInit(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	t.Setenv("DATABASE_URL", "postgres://")
	withArgs(t, "lore", "mcp")

	storeCalled := false
	storeOpen = func(store.Config) (store.Contract, error) {
		storeCalled = true
		return nil, nil
	}

	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdMCP(cfg) })
	if _, ok := recovered.(exitCode); !ok {
		t.Fatalf("expected fatal exit, got %v", recovered)
	}
	if !strings.Contains(stderr, "DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL config error, got %q", stderr)
	}
	if storeCalled {
		t.Fatalf("expected fail-fast before store initialization")
	}
}

func TestCmdServeStagingMissingBaseURLFailsFast(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	t.Setenv("LORE_ENV", "staging")
	t.Setenv("LORE_BASE_URL", "")
	t.Setenv("LORE_JWT_SECRET", "staging-secret-at-least-32-bytes")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_PASSWORD", "bootstrap-secret")
	t.Setenv("DATABASE_URL", "postgres://lore:secret@db.internal:5432/lore")
	withArgs(t, "lore", "serve")

	storeCalled := false
	storeOpen = func(store.Config) (store.Contract, error) {
		storeCalled = true
		return nil, nil
	}

	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdServe(cfg) })

	if _, ok := recovered.(exitCode); !ok {
		t.Fatalf("expected fatal exit, got %v", recovered)
	}
	if !strings.Contains(stderr, "LORE_BASE_URL is required when LORE_ENV=staging") {
		t.Fatalf("expected staging BASE_URL guardrail error, got %q", stderr)
	}
	if storeCalled {
		t.Fatalf("expected fail-fast before store initialization")
	}
}

func TestCmdServeStagingWithRequiredVarsStartsSuccessfully(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)

	t.Setenv("LORE_ENV", "staging")
	t.Setenv("LORE_BASE_URL", "https://staging.lore.local")
	t.Setenv("LORE_JWT_SECRET", "super-secret-at-least-32-bytes-long")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_EMAIL", "admin@staging.lore.local")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_PASSWORD", "bootstrap-secret")
	t.Setenv("LORE_HOST", "")
	t.Setenv("DATABASE_URL", "postgres://lore:secret@db.internal:5432/lore")
	withArgs(t, "lore", "serve")

	storeCalled := false
	startCalled := false
	boundHost := ""

	storeOpen = func(in store.Config) (store.Contract, error) {
		storeCalled = true
		if in.SelectedBackend() != store.BackendPostgreSQL {
			t.Fatalf("SelectedBackend() = %q, want %q", in.SelectedBackend(), store.BackendPostgreSQL)
		}
		return noopStore{}, nil
	}
	newHTTPServer = func(s store.Contract, cfg server.Config) *server.Server {
		boundHost = cfg.Host
		return server.NewWithConfig(s, cfg)
	}
	startHTTP = func(_ *server.Server) error {
		startCalled = true
		return nil
	}

	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdServe(cfg) })
	if recovered != nil {
		t.Fatalf("expected successful startup, got panic=%v stderr=%q", recovered, stderr)
	}
	if !storeCalled {
		t.Fatalf("expected store initialization on staging startup")
	}
	if !startCalled {
		t.Fatalf("expected HTTP server start to be invoked")
	}
	if boundHost != "0.0.0.0" {
		t.Fatalf("expected staging default host 0.0.0.0, got %q", boundHost)
	}
}

func TestCmdServeRailwayStagingUsesPostgresStorageAndPortFallback(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)

	t.Setenv("LORE_ENV", "staging")
	t.Setenv("LORE_HOST", "")
	t.Setenv("LORE_PORT", "")
	t.Setenv("PORT", "8443")
	t.Setenv("LORE_BASE_URL", "https://preview.up.railway.app")
	t.Setenv("LORE_JWT_SECRET", "railway-secret-at-least-32-bytes")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_EMAIL", "admin@railway.local")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_PASSWORD", "bootstrap-secret")
	t.Setenv("DATABASE_URL", "postgres://lore:secret@db.internal:5432/lore")
	withArgs(t, "lore", "serve")

	seenDatabaseURL := ""
	seenBackend := store.BackendSQLite
	seenHost := ""
	seenPort := 0

	storeOpen = func(in store.Config) (store.Contract, error) {
		seenDatabaseURL = in.DatabaseURL
		seenBackend = in.SelectedBackend()
		return noopStore{}, nil
	}
	newHTTPServer = func(s store.Contract, cfg server.Config) *server.Server {
		seenHost = cfg.Host
		seenPort = cfg.Port
		return server.NewWithConfig(s, cfg)
	}
	startHTTP = func(_ *server.Server) error { return nil }

	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdServe(cfg) })
	if recovered != nil {
		t.Fatalf("expected successful startup, got panic=%v stderr=%q", recovered, stderr)
	}
	if seenDatabaseURL != "postgres://lore:secret@db.internal:5432/lore" {
		t.Fatalf("DatabaseURL = %q, want postgres://lore:secret@db.internal:5432/lore", seenDatabaseURL)
	}
	if seenBackend != store.BackendPostgreSQL {
		t.Fatalf("SelectedBackend() = %q, want %q", seenBackend, store.BackendPostgreSQL)
	}
	if seenHost != "0.0.0.0" {
		t.Fatalf("Host = %q, want 0.0.0.0", seenHost)
	}
	if seenPort != 8443 {
		t.Fatalf("Port = %d, want 8443 from PORT fallback", seenPort)
	}
}

func TestCmdServeBootstrapsAdminBeforeStartingHTTP(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)

	t.Setenv("LORE_ENV", "staging")
	t.Setenv("LORE_BASE_URL", "https://staging.lore.local")
	t.Setenv("LORE_JWT_SECRET", "super-secret-at-least-32-bytes-long")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_EMAIL", "admin@example.com")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_PASSWORD", "bootstrap-password")
	t.Setenv("LORE_BOOTSTRAP_ADMIN_NAME", "Bootstrap Admin")
	t.Setenv("DATABASE_URL", "postgres://lore:secret@db.internal:5432/lore")
	withArgs(t, "lore", "serve")

	recorder := &bootstrapRecorderStore{}
	startSawBootstrap := false
	storeOpen = func(in store.Config) (store.Contract, error) {
		return recorder, nil
	}
	startHTTP = func(_ *server.Server) error {
		startSawBootstrap = recorder.called
		if recorder.passwordHash == "" {
			t.Fatalf("expected bootstrap password hash before HTTP start")
		}
		if recorder.passwordHash == "bootstrap-password" {
			t.Fatalf("expected hashed bootstrap password, got plain text")
		}
		return nil
	}

	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdServe(cfg) })
	if recovered != nil {
		t.Fatalf("expected successful startup, got panic=%v stderr=%q", recovered, stderr)
	}
	if !startSawBootstrap {
		t.Fatalf("expected bootstrap admin to run before HTTP start")
	}
	if recorder.email != "admin@example.com" {
		t.Fatalf("bootstrap email = %q, want admin@example.com", recorder.email)
	}
	if recorder.name != "Bootstrap Admin" {
		t.Fatalf("bootstrap name = %q, want Bootstrap Admin", recorder.name)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "short string", in: "abc", max: 10, want: "abc"},
		{name: "exact length", in: "hello", max: 5, want: "hello"},
		{name: "long string", in: "abcdef", max: 3, want: "abc..."},
		{name: "spanish accents", in: "Decisión de arquitectura", max: 8, want: "Decisión..."},
		{name: "emoji", in: "🐛🔧🚀✨🎉💡", max: 3, want: "🐛🔧🚀..."},
		{name: "mixed ascii and multibyte", in: "café☕latte", max: 5, want: "café☕..."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.in, tc.max)
			if got != tc.want {
				t.Fatalf("truncate(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
		})
	}
}

func TestPrintUsage(t *testing.T) {
	oldVersion := version
	version = "test-version"
	t.Cleanup(func() {
		version = oldVersion
	})

	stdout, stderr := captureOutput(t, func() { printUsage() })
	if stderr != "" {
		t.Fatalf("expected no stderr, got: %q", stderr)
	}
	if !strings.Contains(stdout, "lore vtest-version") {
		t.Fatalf("usage missing version: %q", stdout)
	}
	if !strings.Contains(stdout, "search <query>") || !strings.Contains(stdout, "setup [agent]") {
		t.Fatalf("usage missing expected commands: %q", stdout)
	}
	if !strings.Contains(stdout, "Launch local SQLite/dev browser UI") {
		t.Fatalf("usage should frame tui as a local-only browser surface: %q", stdout)
	}
	if strings.Contains(stdout, "Install/setup agent integration") {
		t.Fatalf("usage should not advertise setup ownership: %q", stdout)
	}
}

func TestPrintSetupDeprecation(t *testing.T) {
	stdout, stderr := captureOutput(t, func() { printSetupDeprecation("codex") })
	if stderr != "" {
		t.Fatalf("expected no stderr, got: %q", stderr)
	}
	for _, expected := range []string{"deprecated", "codex", "external configurator", "lore mcp", "lore serve", "no file writes"} {
		if !strings.Contains(strings.ToLower(stdout), strings.ToLower(expected)) {
			t.Fatalf("output missing %q: %q", expected, stdout)
		}
	}
}

func TestCmdSaveAndSearch(t *testing.T) {
	cfg := testConfig(t)

	withArgs(t,
		"engram", "save", "my-title", "my-content",
		"--type", "bugfix",
		"--project", "alpha",
		"--scope", "personal",
		"--topic", "auth/token",
	)

	stdout, stderr := captureOutput(t, func() { cmdSave(cfg) })
	if stderr != "" {
		t.Fatalf("expected no stderr, got: %q", stderr)
	}
	if !strings.Contains(stdout, "Memory saved:") || !strings.Contains(stdout, "my-title") {
		t.Fatalf("unexpected save output: %q", stdout)
	}

	withArgs(t, "lore", "search", "my-content", "--type", "bugfix", "--project", "alpha", "--scope", "personal", "--limit", "1")
	searchOut, searchErr := captureOutput(t, func() { cmdSearch(cfg) })
	if searchErr != "" {
		t.Fatalf("expected no stderr from search, got: %q", searchErr)
	}
	if !strings.Contains(searchOut, "Found 1 memories") || !strings.Contains(searchOut, "my-title") {
		t.Fatalf("unexpected search output: %q", searchOut)
	}

	withArgs(t, "lore", "search", "definitely-not-found")
	noneOut, noneErr := captureOutput(t, func() { cmdSearch(cfg) })
	if noneErr != "" {
		t.Fatalf("expected no stderr from empty search, got: %q", noneErr)
	}
	if !strings.Contains(noneOut, "No memories found") {
		t.Fatalf("expected empty search message, got: %q", noneOut)
	}
}

func TestCmdTimeline(t *testing.T) {
	cfg := testConfig(t)
	mustSeedObservation(t, cfg, "s-1", "proj", "note", "first", "first content", "project")
	focusID := mustSeedObservation(t, cfg, "s-1", "proj", "note", "focus", "focus content", "project")
	mustSeedObservation(t, cfg, "s-1", "proj", "note", "third", "third content", "project")

	withArgs(t, "lore", "timeline", strconv.FormatInt(focusID, 10), "--before", "1", "--after", "1")
	stdout, stderr := captureOutput(t, func() { cmdTimeline(cfg) })
	if stderr != "" {
		t.Fatalf("expected no stderr, got: %q", stderr)
	}
	if !strings.Contains(stdout, "Session:") || !strings.Contains(stdout, ">>> #"+strconv.FormatInt(focusID, 10)) {
		t.Fatalf("timeline output missing expected focus/session info: %q", stdout)
	}
	if !strings.Contains(stdout, "Before") || !strings.Contains(stdout, "After") {
		t.Fatalf("timeline output missing before/after sections: %q", stdout)
	}
}

func TestCmdContextAndStats(t *testing.T) {
	cfg := testConfig(t)

	withArgs(t, "lore", "context")
	emptyCtxOut, emptyCtxErr := captureOutput(t, func() { cmdContext(cfg) })
	if emptyCtxErr != "" {
		t.Fatalf("expected no stderr for empty context, got: %q", emptyCtxErr)
	}
	if !strings.Contains(emptyCtxOut, "No previous session memories found") {
		t.Fatalf("unexpected empty context output: %q", emptyCtxOut)
	}

	mustSeedObservation(t, cfg, "s-ctx", "project-x", "decision", "title", "content", "project")

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	_, err = s.AddPrompt(store.AddPromptParams{SessionID: "s-ctx", Content: "user asked about context", Project: "project-x"})
	if err != nil {
		t.Fatalf("AddPrompt: %v", err)
	}
	_ = s.Close()

	withArgs(t, "lore", "context", "project-x")
	ctxOut, ctxErr := captureOutput(t, func() { cmdContext(cfg) })
	if ctxErr != "" {
		t.Fatalf("expected no stderr for populated context, got: %q", ctxErr)
	}
	if !strings.Contains(ctxOut, "## Memory from Previous Sessions") || !strings.Contains(ctxOut, "Recent Observations") {
		t.Fatalf("unexpected populated context output: %q", ctxOut)
	}

	withArgs(t, "lore", "stats")
	statsOut, statsErr := captureOutput(t, func() { cmdStats(cfg) })
	if statsErr != "" {
		t.Fatalf("expected no stderr from stats, got: %q", statsErr)
	}
	if !strings.Contains(statsOut, "Lore Memory Stats") || !strings.Contains(statsOut, "project-x") {
		t.Fatalf("unexpected stats output: %q", statsOut)
	}
}

func TestCmdExportAndImport(t *testing.T) {
	sourceCfg := testConfig(t)
	targetCfg := testConfig(t)

	mustSeedObservation(t, sourceCfg, "s-exp", "proj-exp", "pattern", "exported", "export me", "project")

	exportPath := filepath.Join(t.TempDir(), "memories.json")

	withArgs(t, "lore", "export", exportPath)
	exportOut, exportErr := captureOutput(t, func() { cmdExport(sourceCfg) })
	if exportErr != "" {
		t.Fatalf("expected no stderr from export, got: %q", exportErr)
	}
	if !strings.Contains(exportOut, "Exported to "+exportPath) {
		t.Fatalf("unexpected export output: %q", exportOut)
	}

	withArgs(t, "lore", "import", exportPath)
	importOut, importErr := captureOutput(t, func() { cmdImport(targetCfg) })
	if importErr != "" {
		t.Fatalf("expected no stderr from import, got: %q", importErr)
	}
	if !strings.Contains(importOut, "Imported from "+exportPath) {
		t.Fatalf("unexpected import output: %q", importOut)
	}

	s, err := store.New(targetCfg)
	if err != nil {
		t.Fatalf("store.New target: %v", err)
	}
	defer s.Close()

	results, err := s.Search("export", store.SearchOptions{Limit: 10, Project: "proj-exp"})
	if err != nil {
		t.Fatalf("Search after import: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected imported data to be searchable")
	}
}

func TestCmdSyncStatusExportAndImport(t *testing.T) {
	workDir := t.TempDir()
	withCwd(t, workDir)

	exportCfg := testConfig(t)
	importCfg := testConfig(t)

	mustSeedObservation(t, exportCfg, "s-sync", "sync-project", "note", "sync title", "sync content", "project")

	withArgs(t, "lore", "sync", "--status")
	statusOut, statusErr := captureOutput(t, func() { cmdSync(exportCfg) })
	if statusErr != "" {
		t.Fatalf("expected no stderr from status, got: %q", statusErr)
	}
	if !strings.Contains(statusOut, "Sync status:") {
		t.Fatalf("unexpected status output: %q", statusOut)
	}

	withArgs(t, "lore", "sync", "--all")
	exportOut, exportErr := captureOutput(t, func() { cmdSync(exportCfg) })
	if exportErr != "" {
		t.Fatalf("expected no stderr from sync export, got: %q", exportErr)
	}
	if !strings.Contains(exportOut, "Created chunk") {
		t.Fatalf("unexpected sync export output: %q", exportOut)
	}

	withArgs(t, "lore", "sync", "--import")
	importOut, importErr := captureOutput(t, func() { cmdSync(importCfg) })
	if importErr != "" {
		t.Fatalf("expected no stderr from sync import, got: %q", importErr)
	}
	if !strings.Contains(importOut, "Imported 1 new chunk(s)") {
		t.Fatalf("unexpected sync import output: %q", importOut)
	}

	withArgs(t, "lore", "sync", "--import")
	noopOut, noopErr := captureOutput(t, func() { cmdSync(importCfg) })
	if noopErr != "" {
		t.Fatalf("expected no stderr from second sync import, got: %q", noopErr)
	}
	if !strings.Contains(noopOut, "No new chunks to import") {
		t.Fatalf("unexpected second sync import output: %q", noopOut)
	}
}

func TestCmdSyncDefaultProjectNoData(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "repo-name")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	withCwd(t, workDir)

	cfg := testConfig(t)
	withArgs(t, "lore", "sync")
	stdout, stderr := captureOutput(t, func() { cmdSync(cfg) })
	if stderr != "" {
		t.Fatalf("expected no stderr, got: %q", stderr)
	}
	if !strings.Contains(stdout, `Exporting memories for project "repo-name"`) {
		t.Fatalf("expected default project message, got: %q", stdout)
	}
	if !strings.Contains(stdout, `Nothing new to sync for project "repo-name"`) {
		t.Fatalf("expected no-data sync message, got: %q", stdout)
	}
}

func TestMainVersionAndHelpAliases(t *testing.T) {
	oldVersion := version
	version = "9.9.9-test"
	t.Cleanup(func() { version = oldVersion })
	stubCheckForUpdates(t, versioncheck.CheckResult{Status: versioncheck.StatusUpToDate})

	tests := []struct {
		name      string
		arg       string
		contains  string
		notStderr bool
	}{
		{name: "version", arg: "version", contains: "lore 9.9.9-test", notStderr: true},
		{name: "version short", arg: "-v", contains: "lore 9.9.9-test", notStderr: true},
		{name: "version long", arg: "--version", contains: "lore 9.9.9-test", notStderr: true},
		{name: "help", arg: "help", contains: "Usage:", notStderr: true},
		{name: "help short", arg: "-h", contains: "Commands:", notStderr: true},
		{name: "help long", arg: "--help", contains: "Environment:", notStderr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withArgs(t, "lore", tc.arg)
			stdout, stderr := captureOutput(t, func() { main() })
			if tc.notStderr && stderr != "" {
				t.Fatalf("expected no stderr, got: %q", stderr)
			}
			if !strings.Contains(stdout, tc.contains) {
				t.Fatalf("stdout %q does not include %q", stdout, tc.contains)
			}
		})
	}
}

func TestMainPrintsUpdateFailuresAndUpdates(t *testing.T) {
	oldVersion := version
	version = "1.10.7"
	t.Cleanup(func() { version = oldVersion })

	t.Run("prints check failure", func(t *testing.T) {
		stubCheckForUpdates(t, versioncheck.CheckResult{
			Status:  versioncheck.StatusCheckFailed,
			Message: "Could not check for updates: GitHub took too long to respond.",
		})
		withArgs(t, "lore", "version")

		stdout, stderr := captureOutput(t, func() { main() })
		if !strings.Contains(stdout, "lore 1.10.7") {
			t.Fatalf("stdout = %q", stdout)
		}
		if !strings.Contains(stderr, "Could not check for updates") {
			t.Fatalf("stderr = %q", stderr)
		}
	})

	t.Run("prints available update", func(t *testing.T) {
		stubCheckForUpdates(t, versioncheck.CheckResult{
			Status:  versioncheck.StatusUpdateAvailable,
			Message: "Update available: 1.10.7 -> 1.10.8",
		})
		withArgs(t, "lore", "version")

		stdout, stderr := captureOutput(t, func() { main() })
		if !strings.Contains(stdout, "lore 1.10.7") {
			t.Fatalf("stdout = %q", stdout)
		}
		if !strings.Contains(stderr, "Update available") {
			t.Fatalf("stderr = %q", stderr)
		}
	})

	t.Run("prints nothing when up to date", func(t *testing.T) {
		stubCheckForUpdates(t, versioncheck.CheckResult{Status: versioncheck.StatusUpToDate})
		withArgs(t, "lore", "version")

		stdout, stderr := captureOutput(t, func() { main() })
		if !strings.Contains(stdout, "lore 1.10.7") {
			t.Fatalf("stdout = %q", stdout)
		}
		if stderr != "" {
			t.Fatalf("stderr = %q, want empty", stderr)
		}
	})
}

func TestMainExitPaths(t *testing.T) {
	tests := []struct {
		name            string
		helperCase      string
		expectedOutput  string
		expectedStderr  string
		expectedExitOne bool
	}{
		{name: "no args", helperCase: "no-args", expectedOutput: "Usage:", expectedExitOne: true},
		{name: "unknown command", helperCase: "unknown", expectedOutput: "Usage:", expectedStderr: "unknown command:", expectedExitOne: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=TestMainExitHelper")
			cmd.Env = append(os.Environ(),
				"GO_WANT_HELPER_PROCESS=1",
				"HELPER_CASE="+tc.helperCase,
			)

			out, err := cmd.CombinedOutput()
			if tc.expectedExitOne {
				exitErr, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("expected exit error, got %T (%v)", err, err)
				}
				if exitErr.ExitCode() != 1 {
					t.Fatalf("expected exit code 1, got %d; output=%q", exitErr.ExitCode(), string(out))
				}
			}

			if !strings.Contains(string(out), tc.expectedOutput) {
				t.Fatalf("output missing %q: %q", tc.expectedOutput, string(out))
			}
			if tc.expectedStderr != "" && !strings.Contains(string(out), tc.expectedStderr) {
				t.Fatalf("output missing stderr text %q: %q", tc.expectedStderr, string(out))
			}
		})
	}
}

func TestMainExitHelper(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	switch os.Getenv("HELPER_CASE") {
	case "no-args":
		os.Args = []string{"engram"}
	case "unknown":
		os.Args = []string{"lore", "definitely-unknown-command"}
	default:
		os.Args = []string{"lore", "--help"}
	}

	main()
}

func TestCmdSearchLocalMode(t *testing.T) {
	cfg := testConfig(t)
	mustSeedObservation(t, cfg, "s-local", "proj-local", "note", "local-result", "local content for search", "project")

	withArgs(t, "lore", "search", "local", "--project", "proj-local")
	stdout, stderr := captureOutput(t, func() { cmdSearch(cfg) })
	if stderr != "" {
		t.Fatalf("expected no stderr, got: %q", stderr)
	}
	if !strings.Contains(stdout, "Found") && !strings.Contains(stdout, "local-result") {
		t.Fatalf("expected local search results, got: %q", stdout)
	}
}

// ─── Projects command tests ───────────────────────────────────────────────────

func TestCmdProjectsListEmpty(t *testing.T) {
	cfg := testConfig(t)

	withArgs(t, "lore", "projects", "list")
	stdout, stderr := captureOutput(t, func() { cmdProjectsList(cfg) })
	if stderr != "" {
		t.Fatalf("expected no stderr, got: %q", stderr)
	}
	if !strings.Contains(stdout, "No projects found") {
		t.Fatalf("expected empty projects message, got: %q", stdout)
	}
}

func TestCmdProjectsList(t *testing.T) {
	cfg := testConfig(t)

	// Seed observations for two projects
	mustSeedObservation(t, cfg, "s-alpha", "alpha", "note", "alpha-note", "alpha content", "project")
	mustSeedObservation(t, cfg, "s-alpha", "alpha", "bugfix", "alpha-bug", "alpha bug", "project")
	mustSeedObservation(t, cfg, "s-beta", "beta", "decision", "beta-note", "beta content", "project")

	withArgs(t, "lore", "projects", "list")
	stdout, stderr := captureOutput(t, func() { cmdProjectsList(cfg) })
	if stderr != "" {
		t.Fatalf("expected no stderr, got: %q", stderr)
	}
	if !strings.Contains(stdout, "Projects (2)") {
		t.Fatalf("expected 'Projects (2)', got: %q", stdout)
	}
	if !strings.Contains(stdout, "alpha") || !strings.Contains(stdout, "beta") {
		t.Fatalf("expected project names in output, got: %q", stdout)
	}
	// alpha has 2 observations, beta has 1 — alpha should appear first
	alphaIdx := strings.Index(stdout, "alpha")
	betaIdx := strings.Index(stdout, "beta")
	if alphaIdx > betaIdx {
		t.Fatalf("expected alpha (more obs) before beta, got: %q", stdout)
	}
}

func TestCmdProjectsRoutesSubcommands(t *testing.T) {
	cfg := testConfig(t)

	// "list" subcommand
	withArgs(t, "lore", "projects", "list")
	stdout, _ := captureOutput(t, func() { cmdProjects(cfg) })
	if !strings.Contains(stdout, "No projects found") && !strings.Contains(stdout, "Projects") {
		t.Fatalf("expected projects list output, got: %q", stdout)
	}

	// default (no subcommand) → list
	withArgs(t, "lore", "projects")
	stdout2, _ := captureOutput(t, func() { cmdProjects(cfg) })
	_ = stdout2 // just checking it doesn't crash
}

func TestCmdProjectsConsolidateNoSimilar(t *testing.T) {
	cfg := testConfig(t)

	// Seed a single unique project
	mustSeedObservation(t, cfg, "s-unique", "unique-project", "note", "unique note", "content", "project")

	// Set cwd to a temp dir named "unique-project" with no git
	workDir := filepath.Join(t.TempDir(), "unique-project")
	if err := os.MkdirAll(workDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	withCwd(t, workDir)

	// Stub detectProject to return the known canonical
	old := detectProject
	detectProject = func(string) string { return "unique-project" }
	t.Cleanup(func() { detectProject = old })

	withArgs(t, "lore", "projects", "consolidate")
	stdout, stderr := captureOutput(t, func() { cmdProjectsConsolidate(cfg) })
	if stderr != "" {
		t.Fatalf("expected no stderr, got: %q", stderr)
	}
	if !strings.Contains(stdout, "No similar") {
		t.Fatalf("expected no-similar message, got: %q", stdout)
	}
}

func TestCmdProjectsConsolidateDryRun(t *testing.T) {
	cfg := testConfig(t)

	// Seed a canonical and a similar variant (substring match, distinct after normalize)
	mustSeedObservation(t, cfg, "s-eng", "engram", "note", "eng note", "content", "project")
	mustSeedObservation(t, cfg, "s-engm", "engram-memory", "note", "engm note", "content", "project")

	old := detectProject
	detectProject = func(string) string { return "engram" }
	t.Cleanup(func() { detectProject = old })

	withArgs(t, "lore", "projects", "consolidate", "--dry-run")
	stdout, stderr := captureOutput(t, func() { cmdProjectsConsolidate(cfg) })
	if stderr != "" {
		t.Fatalf("expected no stderr, got: %q", stderr)
	}
	if !strings.Contains(stdout, "dry-run") {
		t.Fatalf("expected dry-run message, got: %q", stdout)
	}
	// Verify no actual merge happened (both projects still exist)
	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer s.Close()
	names, err := s.ListProjectNames()
	if err != nil {
		t.Fatalf("ListProjectNames: %v", err)
	}
	// Should still have both names (no merge happened)
	if len(names) < 2 {
		t.Fatalf("expected 2 project names after dry-run, got: %v", names)
	}
}

func TestCmdProjectsConsolidateSingleProject(t *testing.T) {
	cfg := testConfig(t)

	// Seed canonical and a similar variant (substring match, distinct after normalize)
	mustSeedObservation(t, cfg, "s-eng", "engram", "note", "eng note", "content", "project")
	mustSeedObservation(t, cfg, "s-engm", "engram-memory", "note", "engm note", "content", "project")

	old := detectProject
	detectProject = func(string) string { return "engram" }
	t.Cleanup(func() { detectProject = old })

	// Stub scanInputLine to answer "all"
	oldScan := scanInputLine
	t.Cleanup(func() { scanInputLine = oldScan })
	scanInputLine = func(a ...any) (int, error) {
		if ptr, ok := a[0].(*string); ok {
			*ptr = "all"
		}
		return 1, nil
	}

	withArgs(t, "lore", "projects", "consolidate")
	stdout, stderr := captureOutput(t, func() { cmdProjectsConsolidate(cfg) })
	if stderr != "" {
		t.Fatalf("expected no stderr, got: %q", stderr)
	}
	if !strings.Contains(stdout, "Merged into") {
		t.Fatalf("expected merge result, got: %q", stdout)
	}

	// Verify engram-memory was merged into engram
	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	defer s.Close()
	names, err := s.ListProjectNames()
	if err != nil {
		t.Fatalf("ListProjectNames: %v", err)
	}
	if len(names) != 1 || names[0] != "engram" {
		t.Fatalf("expected only 'engram' after merge, got: %v", names)
	}
}

func TestCmdProjectsConsolidateAllDryRun(t *testing.T) {
	cfg := testConfig(t)

	// Seed similar projects (substring match, stays distinct after normalize)
	mustSeedObservation(t, cfg, "s-eng", "engram", "note", "eng note", "content", "project")
	mustSeedObservation(t, cfg, "s-engm", "engram-memory", "note", "engm note", "content", "project")

	withArgs(t, "lore", "projects", "consolidate", "--all", "--dry-run")
	stdout, stderr := captureOutput(t, func() { cmdProjectsConsolidate(cfg) })
	if stderr != "" {
		t.Fatalf("expected no stderr, got: %q", stderr)
	}
	if !strings.Contains(stdout, "dry-run") || !strings.Contains(stdout, "Group") {
		t.Fatalf("expected dry-run group output, got: %q", stdout)
	}
}

func TestCmdProjectsAllNoGroups(t *testing.T) {
	cfg := testConfig(t)

	// Seed completely unrelated projects
	mustSeedObservation(t, cfg, "s-foo", "fooproject", "note", "foo", "content", "project")
	mustSeedObservation(t, cfg, "s-bar", "barproject", "note", "bar", "content", "project")
	mustSeedObservation(t, cfg, "s-qux", "quxproject", "note", "qux", "content", "project")

	withArgs(t, "lore", "projects", "consolidate", "--all")
	stdout, stderr := captureOutput(t, func() { cmdProjectsConsolidate(cfg) })
	if stderr != "" {
		t.Fatalf("expected no stderr, got: %q", stderr)
	}
	// The three "project"-suffixed names might be grouped by similarity.
	// We just verify it runs without error and produces readable output.
	_ = stdout
}

func TestCmdMCPDetectsProjectFromFlag(t *testing.T) {
	// Test that --project flag is parsed and passed to MCP config.
	// We can't easily test the full MCP server startup (it blocks on stdio),
	// but we test the flag-parsing + detectProject chain indirectly by
	// checking that cmdMCP doesn't crash when store is available.
	//
	// The key invariant tested: --project sets detectedProject correctly.
	// We verify by stubbing newMCPServerWithConfig and checking the MCPConfig.
	cfg := testConfig(t)

	var capturedCfg mcp.MCPConfig
	oldNew := newMCPServerWithConfig
	t.Cleanup(func() { newMCPServerWithConfig = oldNew })
	newMCPServerWithConfig = func(s store.Contract, mcpCfg mcp.MCPConfig, allowlist map[string]bool) *mcpserver.MCPServer {
		capturedCfg = mcpCfg
		// Return a valid server so serveMCP doesn't panic
		return oldNew(s, mcpCfg, allowlist)
	}

	oldServe := serveMCP
	t.Cleanup(func() { serveMCP = oldServe })
	// Prevent actual stdio serve — return immediately
	serveMCP = func(srv *mcpserver.MCPServer, opts ...mcpserver.StdioOption) error {
		return nil
	}

	withArgs(t, "lore", "mcp", "--project=myproject")
	_, _ = captureOutput(t, func() { cmdMCP(cfg) })

	if capturedCfg.DefaultProject != "myproject" {
		t.Fatalf("expected DefaultProject=%q, got %q", "myproject", capturedCfg.DefaultProject)
	}
}

func TestCmdMCPDetectsProjectFromEnv(t *testing.T) {
	cfg := testConfig(t)

	t.Setenv("LORE_PROJECT", "env-project")

	var capturedCfg mcp.MCPConfig
	oldNew := newMCPServerWithConfig
	t.Cleanup(func() { newMCPServerWithConfig = oldNew })
	newMCPServerWithConfig = func(s store.Contract, mcpCfg mcp.MCPConfig, allowlist map[string]bool) *mcpserver.MCPServer {
		capturedCfg = mcpCfg
		return oldNew(s, mcpCfg, allowlist)
	}

	oldServe := serveMCP
	t.Cleanup(func() { serveMCP = oldServe })
	serveMCP = func(srv *mcpserver.MCPServer, opts ...mcpserver.StdioOption) error {
		return nil
	}

	withArgs(t, "lore", "mcp")
	_, _ = captureOutput(t, func() { cmdMCP(cfg) })

	if capturedCfg.DefaultProject != "env-project" {
		t.Fatalf("expected DefaultProject=%q, got %q", "env-project", capturedCfg.DefaultProject)
	}
}

func TestCmdMCPDetectsProjectFromGit(t *testing.T) {
	cfg := testConfig(t)

	// Stub detectProject to simulate git detection
	old := detectProject
	t.Cleanup(func() { detectProject = old })
	detectProject = func(string) string { return "detected-from-git" }

	var capturedCfg mcp.MCPConfig
	oldNew := newMCPServerWithConfig
	t.Cleanup(func() { newMCPServerWithConfig = oldNew })
	newMCPServerWithConfig = func(s store.Contract, mcpCfg mcp.MCPConfig, allowlist map[string]bool) *mcpserver.MCPServer {
		capturedCfg = mcpCfg
		return oldNew(s, mcpCfg, allowlist)
	}

	oldServe := serveMCP
	t.Cleanup(func() { serveMCP = oldServe })
	serveMCP = func(srv *mcpserver.MCPServer, opts ...mcpserver.StdioOption) error {
		return nil
	}

	withArgs(t, "lore", "mcp")
	_, _ = captureOutput(t, func() { cmdMCP(cfg) })

	if capturedCfg.DefaultProject != "detected-from-git" {
		t.Fatalf("expected DefaultProject=%q, got %q", "detected-from-git", capturedCfg.DefaultProject)
	}
}

func TestCmdSyncUsesDetectProject(t *testing.T) {
	workDir := t.TempDir()
	withCwd(t, workDir)

	cfg := testConfig(t)

	// Stub detectProject to verify it's called instead of filepath.Base
	old := detectProject
	t.Cleanup(func() { detectProject = old })
	detectProject = func(dir string) string { return "git-detected-project" }

	withArgs(t, "lore", "sync")
	stdout, stderr := captureOutput(t, func() { cmdSync(cfg) })
	if stderr != "" {
		t.Fatalf("expected no stderr, got: %q", stderr)
	}
	if !strings.Contains(stdout, "git-detected-project") {
		t.Fatalf("expected detectProject result in output, got: %q", stdout)
	}
}

// ─── obsidian-export command tests ───────────────────────────────────────────

// TestObsidianExportMissingVault verifies that omitting --vault exits with code 1
// and prints an error message to stderr (REQ-EXPORT-01: missing --vault scenario).
func TestObsidianExportMissingVault(t *testing.T) {
	cfg := testConfig(t)

	var exitCode int
	oldExit := exitFunc
	t.Cleanup(func() { exitFunc = oldExit })
	exitFunc = func(code int) { exitCode = code; panic("exit") }

	withArgs(t, "lore", "obsidian-export", "--project", "eng")

	// Capture stderr before the panic unwinds by closing pipes inside captureOutput.
	// We use a wrapper that recovers from the exitFunc panic and then still closes
	// the write-end pipes so ReadAll can drain them.
	oldOut := os.Stdout
	oldErr := os.Stderr
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	os.Stdout = outW
	os.Stderr = errW

	func() {
		defer func() {
			recover() //nolint:errcheck
		}()
		cmdObsidianExport(cfg)
	}()

	_ = outW.Close()
	_ = errW.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr

	errBytes, _ := io.ReadAll(errR)
	_, _ = io.ReadAll(outR)
	stderr := string(errBytes)

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr, "--vault") {
		t.Fatalf("expected '--vault' in stderr, got: %q", stderr)
	}
}

// TestObsidianExportCallsInjectedExporter verifies that when --vault is provided,
// the injected newObsidianExporter is called with the correct config
// (REQ-EXPORT-01: happy path with all flags).
func TestObsidianExportCallsInjectedExporter(t *testing.T) {
	cfg := testConfig(t)
	vaultDir := t.TempDir()

	// Track the ExportConfig passed to the injected constructor
	var capturedCfg obsidian.ExportConfig
	exporterCalled := false

	oldNew := newObsidianExporter
	t.Cleanup(func() { newObsidianExporter = oldNew })
	newObsidianExporter = func(s obsidian.StoreReader, c obsidian.ExportConfig) *obsidian.Exporter {
		capturedCfg = c
		exporterCalled = true
		return obsidian.NewExporter(s, c)
	}

	withArgs(t, "lore", "obsidian-export",
		"--vault", vaultDir,
		"--project", "eng",
		"--limit", "50",
		"--since", "2026-01-01",
	)

	_, _ = captureOutput(t, func() { cmdObsidianExport(cfg) })

	if !exporterCalled {
		t.Fatalf("expected newObsidianExporter to be called")
	}
	if capturedCfg.VaultPath != vaultDir {
		t.Fatalf("expected VaultPath=%q, got %q", vaultDir, capturedCfg.VaultPath)
	}
	if capturedCfg.Project != "eng" {
		t.Fatalf("expected Project=%q, got %q", "eng", capturedCfg.Project)
	}
	if capturedCfg.Limit != 50 {
		t.Fatalf("expected Limit=50, got %d", capturedCfg.Limit)
	}
	if capturedCfg.Since.IsZero() {
		t.Fatalf("expected Since to be set from --since 2026-01-01, got zero")
	}
}

// TestObsidianExportMinimalFlags verifies that only --vault (the required flag)
// is sufficient — optional flags default to zero values (triangulation case).
func TestObsidianExportMinimalFlags(t *testing.T) {
	cfg := testConfig(t)
	vaultDir := t.TempDir()

	var capturedCfg obsidian.ExportConfig
	oldNew := newObsidianExporter
	t.Cleanup(func() { newObsidianExporter = oldNew })
	newObsidianExporter = func(s obsidian.StoreReader, c obsidian.ExportConfig) *obsidian.Exporter {
		capturedCfg = c
		return obsidian.NewExporter(s, c)
	}

	withArgs(t, "lore", "obsidian-export", "--vault", vaultDir)

	_, _ = captureOutput(t, func() { cmdObsidianExport(cfg) })

	if capturedCfg.VaultPath != vaultDir {
		t.Fatalf("expected VaultPath=%q, got %q", vaultDir, capturedCfg.VaultPath)
	}
	// Optional flags should be zero
	if capturedCfg.Project != "" {
		t.Fatalf("expected empty Project, got %q", capturedCfg.Project)
	}
	if capturedCfg.Limit != 0 {
		t.Fatalf("expected Limit=0, got %d", capturedCfg.Limit)
	}
	if !capturedCfg.Since.IsZero() {
		t.Fatalf("expected Since=zero, got %v", capturedCfg.Since)
	}
}

// TestObsidianExportInHelpText verifies that "obsidian-export" appears in printUsage output.
func TestObsidianExportInHelpText(t *testing.T) {
	stdout, _ := captureOutput(t, func() { printUsage() })
	if !strings.Contains(stdout, "obsidian-export") {
		t.Fatalf("expected 'obsidian-export' in help text, got: %q", stdout)
	}
}

// ─── obsidian-export Phase 4 tests (graph-config, watch, interval) ───────────

// captureExitPanic is a helper that runs fn inside a panic-recovering wrapper,
// captures stdout/stderr via os.Pipe, and returns the exit code (via exitFunc stub).
func captureExitPanic(t *testing.T, fn func()) (stdout, stderr string, exitCode int) {
	t.Helper()

	oldExit := exitFunc
	t.Cleanup(func() { exitFunc = oldExit })
	exitFunc = func(code int) { exitCode = code; panic("exit") }

	oldOut := os.Stdout
	oldErr := os.Stderr
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	os.Stdout = outW
	os.Stderr = errW

	func() {
		defer func() { recover() }() //nolint:errcheck
		fn()
	}()

	_ = outW.Close()
	_ = errW.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr

	outBytes, _ := io.ReadAll(outR)
	errBytes, _ := io.ReadAll(errR)
	return string(outBytes), string(errBytes), exitCode
}

// TestObsidianExportGraphConfigInvalid verifies that --graph-config with an
// invalid value exits 1 and prints an error to stderr. (REQ-GRAPH-01)
func TestObsidianExportGraphConfigInvalid(t *testing.T) {
	cfg := testConfig(t)
	vaultDir := t.TempDir()

	withArgs(t, "lore", "obsidian-export",
		"--vault", vaultDir,
		"--graph-config", "bananas",
	)

	_, stderr, code := captureExitPanic(t, func() { cmdObsidianExport(cfg) })

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr, "graph-config") {
		t.Fatalf("expected 'graph-config' in stderr, got: %q", stderr)
	}
}

// TestObsidianExportGraphConfigDefaultsToPreserve verifies that when --graph-config
// is not set, the exporter is called with GraphConfigPreserve. (REQ-GRAPH-01)
func TestObsidianExportGraphConfigDefaultsToPreserve(t *testing.T) {
	cfg := testConfig(t)
	vaultDir := t.TempDir()

	var capturedCfg obsidian.ExportConfig
	oldNew := newObsidianExporter
	t.Cleanup(func() { newObsidianExporter = oldNew })
	newObsidianExporter = func(s obsidian.StoreReader, c obsidian.ExportConfig) *obsidian.Exporter {
		capturedCfg = c
		return obsidian.NewExporter(s, c)
	}

	withArgs(t, "lore", "obsidian-export", "--vault", vaultDir)

	_, _ = captureOutput(t, func() { cmdObsidianExport(cfg) })

	if capturedCfg.GraphConfig != obsidian.GraphConfigPreserve {
		t.Fatalf("expected GraphConfig=%q (preserve), got %q", obsidian.GraphConfigPreserve, capturedCfg.GraphConfig)
	}
}

// TestObsidianExportWatchRequiresInterval verifies that --watch alone uses
// the default 10m interval and does NOT exit with an error. (REQ-WATCH-02)
func TestObsidianExportWatchRequiresInterval(t *testing.T) {
	cfg := testConfig(t)
	vaultDir := t.TempDir()

	// Inject a fake watcher that records the call and returns immediately.
	var watcherCalled bool
	var capturedInterval time.Duration
	oldWatcher := newObsidianWatcher
	t.Cleanup(func() { newObsidianWatcher = oldWatcher })
	newObsidianWatcher = func(wc obsidian.WatcherConfig) *obsidian.Watcher {
		watcherCalled = true
		capturedInterval = wc.Interval
		return nil // nil signals the CLI to skip watcher.Run()
	}

	withArgs(t, "lore", "obsidian-export", "--vault", vaultDir, "--watch")

	// --watch with nil watcher should not panic and should not exit 1
	var exitCode int
	oldExit := exitFunc
	t.Cleanup(func() { exitFunc = oldExit })
	exitFunc = func(code int) { exitCode = code; panic("exit") }

	func() {
		defer func() { recover() }() //nolint:errcheck
		_, _ = captureOutput(t, func() { cmdObsidianExport(cfg) })
	}()

	// Exit code should be 0 (clean exit after watcher returns nil)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if !watcherCalled {
		t.Fatalf("expected newObsidianWatcher to be called")
	}
	if capturedInterval != 10*time.Minute {
		t.Fatalf("expected default interval 10m, got %v", capturedInterval)
	}
}

// TestObsidianExportIntervalWithoutWatchErrors verifies that --interval without
// --watch exits 1. (REQ-WATCH-07)
func TestObsidianExportIntervalWithoutWatchErrors(t *testing.T) {
	cfg := testConfig(t)
	vaultDir := t.TempDir()

	withArgs(t, "lore", "obsidian-export",
		"--vault", vaultDir,
		"--interval", "5m",
	)

	_, stderr, code := captureExitPanic(t, func() { cmdObsidianExport(cfg) })

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr, "--interval") && !strings.Contains(stderr, "watch") {
		t.Fatalf("expected '--interval' or 'watch' in stderr, got: %q", stderr)
	}
}

// TestObsidianExportIntervalBelowMinimumErrors verifies that --watch --interval 30s
// exits 1 because the interval is below the 1-minute minimum. (REQ-WATCH-07)
func TestObsidianExportIntervalBelowMinimumErrors(t *testing.T) {
	cfg := testConfig(t)
	vaultDir := t.TempDir()

	withArgs(t, "lore", "obsidian-export",
		"--vault", vaultDir,
		"--watch",
		"--interval", "30s",
	)

	_, stderr, code := captureExitPanic(t, func() { cmdObsidianExport(cfg) })

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr, "1m") && !strings.Contains(stderr, "minimum") {
		t.Fatalf("expected minimum interval message in stderr, got: %q", stderr)
	}
}

// TestObsidianExportIntervalUnparseableErrors verifies that --watch --interval banana
// exits 1 with a parse error. (REQ-WATCH-07)
func TestObsidianExportIntervalUnparseableErrors(t *testing.T) {
	cfg := testConfig(t)
	vaultDir := t.TempDir()

	withArgs(t, "lore", "obsidian-export",
		"--vault", vaultDir,
		"--watch",
		"--interval", "banana",
	)

	_, stderr, code := captureExitPanic(t, func() { cmdObsidianExport(cfg) })

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr, "interval") {
		t.Fatalf("expected 'interval' in stderr, got: %q", stderr)
	}
}

// TestObsidianExportWatchModeCallsInjectedWatcher verifies that with --watch,
// the injected newObsidianWatcher is called with the correct WatcherConfig.
// Uses a fake that records the call. (REQ-WATCH-01)
func TestObsidianExportWatchModeCallsInjectedWatcher(t *testing.T) {
	cfg := testConfig(t)
	vaultDir := t.TempDir()

	var watcherCfg obsidian.WatcherConfig
	watcherCalled := false
	oldWatcher := newObsidianWatcher
	t.Cleanup(func() { newObsidianWatcher = oldWatcher })
	newObsidianWatcher = func(wc obsidian.WatcherConfig) *obsidian.Watcher {
		watcherCalled = true
		watcherCfg = wc
		return nil // nil means Run() is skipped; clean exit
	}

	withArgs(t, "lore", "obsidian-export",
		"--vault", vaultDir,
		"--watch",
		"--interval", "2m",
	)

	var exitCode int
	oldExit := exitFunc
	t.Cleanup(func() { exitFunc = oldExit })
	exitFunc = func(code int) { exitCode = code; panic("exit") }

	func() {
		defer func() { recover() }() //nolint:errcheck
		_, _ = captureOutput(t, func() { cmdObsidianExport(cfg) })
	}()

	if exitCode != 0 {
		t.Fatalf("expected clean exit (0), got %d", exitCode)
	}
	if !watcherCalled {
		t.Fatalf("expected newObsidianWatcher to be called")
	}
	if watcherCfg.Interval != 2*time.Minute {
		t.Fatalf("expected interval 2m, got %v", watcherCfg.Interval)
	}
	if watcherCfg.Exporter == nil {
		t.Fatalf("expected non-nil Exporter in WatcherConfig")
	}
	if watcherCfg.Logf == nil {
		t.Fatalf("expected non-nil Logf in WatcherConfig")
	}
}
