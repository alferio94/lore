package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRootFromCwd(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd(): %v", err)
	}

	root := wd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatalf("could not locate repo root from %s", wd)
		}
		root = parent
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", path, err)
	}
	return string(data)
}

func TestDockerStagingRequiredFilesExist(t *testing.T) {
	root := repoRootFromCwd(t)

	required := []string{
		"Dockerfile",
		"docker-compose.staging.yml",
		".env.example",
		".dockerignore",
	}

	for _, rel := range required {
		rel := rel
		t.Run(rel, func(t *testing.T) {
			if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
				t.Fatalf("required file %q is missing: %v", rel, err)
			}
		})
	}
}

func TestEnvExampleContainsRuntimeAndStorageVariables(t *testing.T) {
	root := repoRootFromCwd(t)
	content := mustReadFile(t, filepath.Join(root, ".env.example"))

	requiredVars := []string{"LORE_ENV", "LORE_BASE_URL", "LORE_JWT_SECRET", "LORE_PORT", "PORT", "DATABASE_URL"}
	for _, requiredVar := range requiredVars {
		requiredVar := requiredVar
		t.Run(requiredVar, func(t *testing.T) {
			if !strings.Contains(content, requiredVar+"=") {
				t.Fatalf(".env.example must include %s", requiredVar)
			}
		})
	}
}

func TestRuntimeDocsDescribePortPrecedenceAndSQLiteBehavior(t *testing.T) {
	root := repoRootFromCwd(t)

	installation := mustReadFile(t, filepath.Join(root, "docs", "INSTALLATION.md"))
	if !strings.Contains(installation, "LORE_PORT") || !strings.Contains(installation, "PORT") {
		t.Fatalf("docs/INSTALLATION.md must describe LORE_PORT and PORT runtime inputs")
	}
	if !strings.Contains(installation, "DATABASE_URL") {
		t.Fatalf("docs/INSTALLATION.md must mention DATABASE_URL acceptance")
	}

	docs := mustReadFile(t, filepath.Join(root, "DOCS.md"))
	if !strings.Contains(docs, "LORE_PORT") || !strings.Contains(docs, "PORT") {
		t.Fatalf("DOCS.md must describe serve port precedence")
	}
	if !strings.Contains(docs, "SQLite") {
		t.Fatalf("DOCS.md must preserve SQLite runtime guidance")
	}
}

func TestDockerComposeIncludesVolumeAndHealthcheckReferences(t *testing.T) {
	root := repoRootFromCwd(t)
	compose := mustReadFile(t, filepath.Join(root, "docker-compose.staging.yml"))

	if !strings.Contains(compose, "lore-data") {
		t.Fatalf("docker-compose.staging.yml must reference lore-data volume")
	}

	if !strings.Contains(compose, "/health") {
		t.Fatalf("docker-compose.staging.yml must reference /health in healthcheck")
	}
}

func TestStagingValidationScriptAndCIComposeCheckPresent(t *testing.T) {
	root := repoRootFromCwd(t)

	scriptPath := filepath.Join(root, "scripts", "validate-staging.sh")
	script := mustReadFile(t, scriptPath)

	requiredScriptChecks := []string{
		"docker-compose.staging.yml",
		".env.example",
		"Dockerfile",
		"docker compose -f",
		"config --quiet",
	}
	for _, check := range requiredScriptChecks {
		if !strings.Contains(script, check) {
			t.Fatalf("scripts/validate-staging.sh must include %q", check)
		}
	}

	ciWorkflow := mustReadFile(t, filepath.Join(root, ".github", "workflows", "ci.yml"))
	if !strings.Contains(ciWorkflow, "docker compose -f docker-compose.staging.yml config --quiet") {
		t.Fatalf("ci workflow must validate compose syntax with docker compose config --quiet")
	}
}
