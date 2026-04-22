package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/alferio94/lore/internal/store"
)

type RuntimeEnv string

const (
	RuntimeEnvLocal   RuntimeEnv = "local"
	RuntimeEnvStaging RuntimeEnv = "staging"
)

type RuntimeConfig struct {
	Env RuntimeEnv

	Host         string
	Port         int
	BaseURL      string
	JWTSecret    string
	CookieSecure bool
	DataDir      string

	GoogleClientID     string
	GoogleClientSecret string
	GitHubClientID     string
	GitHubClientSecret string
}

func loadRuntimeConfig(cfg store.Config, args []string) (RuntimeConfig, error) {
	envRaw := strings.TrimSpace(os.Getenv("LORE_ENV"))
	env := RuntimeEnvLocal
	switch envRaw {
	case "", string(RuntimeEnvLocal):
		env = RuntimeEnvLocal
	case string(RuntimeEnvStaging):
		env = RuntimeEnvStaging
	default:
		return RuntimeConfig{}, fmt.Errorf("lore config: invalid LORE_ENV %q (allowed: local, staging)", envRaw)
	}

	port := 7437
	if p := strings.TrimSpace(os.Getenv("LORE_PORT")); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	for _, arg := range args {
		if arg == "--dev-auth" {
			continue
		}
		if n, err := strconv.Atoi(arg); err == nil {
			port = n
		}
	}

	host := strings.TrimSpace(os.Getenv("LORE_HOST"))
	if host == "" {
		if env == RuntimeEnvStaging {
			host = "0.0.0.0"
		} else {
			host = "127.0.0.1"
		}
	}

	baseURL := strings.TrimSpace(os.Getenv("LORE_BASE_URL"))
	if baseURL == "" {
		if env == RuntimeEnvStaging {
			return RuntimeConfig{}, fmt.Errorf("lore config: LORE_BASE_URL is required when LORE_ENV=staging")
		}
		baseURL = fmt.Sprintf("http://%s:%d", host, port)
	}

	cookieSecure := env == RuntimeEnvStaging
	if v := strings.TrimSpace(os.Getenv("LORE_COOKIE_SECURE")); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return RuntimeConfig{}, fmt.Errorf("lore config: invalid LORE_COOKIE_SECURE %q (allowed: true/false)", v)
		}
		cookieSecure = parsed
	}

	jwtSecret := os.Getenv("LORE_JWT_SECRET")
	if env == RuntimeEnvStaging && strings.TrimSpace(jwtSecret) == "" {
		return RuntimeConfig{}, fmt.Errorf("lore config: LORE_JWT_SECRET is required when LORE_ENV=staging")
	}

	dataDir := cfg.DataDir
	if dir := strings.TrimSpace(os.Getenv("LORE_DATA_DIR")); dir != "" {
		dataDir = dir
	}

	return RuntimeConfig{
		Env:                env,
		Host:               host,
		Port:               port,
		BaseURL:            baseURL,
		JWTSecret:          jwtSecret,
		CookieSecure:       cookieSecure,
		DataDir:            dataDir,
		GoogleClientID:     os.Getenv("LORE_GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("LORE_GOOGLE_CLIENT_SECRET"),
		GitHubClientID:     os.Getenv("LORE_GITHUB_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("LORE_GITHUB_CLIENT_SECRET"),
	}, nil
}
