package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type RuntimeEnv string

const (
	RuntimeEnvLocal          RuntimeEnv = "local"
	RuntimeEnvStaging        RuntimeEnv = "staging"
	minStagingJWTSecretBytes            = 32
)

type RuntimeConfig struct {
	Env RuntimeEnv

	Host         string
	Port         int
	BaseURL      string
	JWTSecret    string
	CookieSecure bool

	BootstrapAdminEmail    string
	BootstrapAdminPassword string
	BootstrapAdminName     string

	GoogleClientID     string
	GoogleClientSecret string
	GitHubClientID     string
	GitHubClientSecret string
}

func loadRuntimeConfig(args []string) (RuntimeConfig, error) {
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

	port := resolveServePort(args)

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
	if env == RuntimeEnvStaging {
		trimmedJWTSecret := strings.TrimSpace(jwtSecret)
		if trimmedJWTSecret == "" {
			return RuntimeConfig{}, fmt.Errorf("lore config: LORE_JWT_SECRET is required when LORE_ENV=staging")
		}
		if len(trimmedJWTSecret) < minStagingJWTSecretBytes {
			return RuntimeConfig{}, fmt.Errorf("lore config: LORE_JWT_SECRET must be at least %d bytes when LORE_ENV=staging", minStagingJWTSecretBytes)
		}
	}

	bootstrapAdminEmail := strings.TrimSpace(os.Getenv("LORE_BOOTSTRAP_ADMIN_EMAIL"))
	if bootstrapAdminEmail == "" && env == RuntimeEnvLocal {
		bootstrapAdminEmail = "admin@admin.com"
	}
	bootstrapAdminPassword := strings.TrimSpace(os.Getenv("LORE_BOOTSTRAP_ADMIN_PASSWORD"))
	if env == RuntimeEnvStaging && bootstrapAdminPassword == "" {
		return RuntimeConfig{}, fmt.Errorf("lore config: LORE_BOOTSTRAP_ADMIN_PASSWORD is required when LORE_ENV=staging")
	}
	if env == RuntimeEnvStaging && bootstrapAdminEmail == "" {
		return RuntimeConfig{}, fmt.Errorf("lore config: LORE_BOOTSTRAP_ADMIN_EMAIL is required when LORE_ENV=staging")
	}
	bootstrapAdminName := strings.TrimSpace(os.Getenv("LORE_BOOTSTRAP_ADMIN_NAME"))

	return RuntimeConfig{
		Env:                    env,
		Host:                   host,
		Port:                   port,
		BaseURL:                baseURL,
		JWTSecret:              jwtSecret,
		CookieSecure:           cookieSecure,
		BootstrapAdminEmail:    bootstrapAdminEmail,
		BootstrapAdminPassword: bootstrapAdminPassword,
		BootstrapAdminName:     bootstrapAdminName,
		GoogleClientID:         os.Getenv("LORE_GOOGLE_CLIENT_ID"),
		GoogleClientSecret:     os.Getenv("LORE_GOOGLE_CLIENT_SECRET"),
		GitHubClientID:         os.Getenv("LORE_GITHUB_CLIENT_ID"),
		GitHubClientSecret:     os.Getenv("LORE_GITHUB_CLIENT_SECRET"),
	}, nil
}

func resolveServePort(args []string) int {
	if argPort, ok := parsePositionalPortArg(args); ok {
		return argPort
	}

	if lorePort, ok := parsePortEnv("LORE_PORT"); ok {
		return lorePort
	}

	if port, ok := parsePortEnv("PORT"); ok {
		return port
	}

	return 7437
}

func parsePositionalPortArg(args []string) (int, bool) {
	for _, arg := range args {
		if arg == "--dev-auth" {
			continue
		}
		n, err := strconv.Atoi(arg)
		if err != nil {
			continue
		}
		return n, true
	}
	return 0, false
}

func parsePortEnv(key string) (int, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return n, true
}
