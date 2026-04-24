package main

import (
	"path/filepath"
	"strings"
	"testing"
)

type docsContract struct {
	Path           string
	MustContain    []string
	MustNotContain []string
}

func cloudFirstDocsContract() []docsContract {
	commonForbidden := []string{
		"local-first",
		"setup-first",
		"plugin/opencode",
		"plugin/claude-code",
		".claude-plugin",
		"internal/setup/plugins",
		"claude plugin marketplace add",
		"~/.claude/settings.json",
		"~/.gemini/system.md",
		"~/.codex/lore-instructions.md",
	}

	return []docsContract{
		{
			Path: "README.md",
			MustContain: []string{
				"Hosted/runtime workflows come first.",
				"Local compatibility mode",
				"Vendor-specific setup, plugin packaging, and configurator UX belong outside this repo.",
				"`lore setup [agent]` is retained only as a compatibility stub.",
				"Railway preview runtime",
				"`PORT`, `DATABASE_URL`, `LORE_ENV`, `LORE_BASE_URL`, and `LORE_JWT_SECRET`",
				"`/health` and `/mcp` smoke check",
				"No web view/dashboard/browser UI expansion, TUI changes, agent configurators/plugins, or production auth/multi-user hardening are included in this preview.",
			},
			MustNotContain: commonForbidden,
		},
		{
			Path: "DOCS.md",
			MustContain: []string{
				"cloud-first runtime surfaces",
				"SQLite and `lore tui` remain supported for local development, testing, demos, and compatibility workflows.",
				"These are the stable Lore-owned primitives that external configurators should target.",
				"Deprecated compatibility stub only",
			},
			MustNotContain: commonForbidden,
		},
		{
			Path: "docs/ARCHITECTURE.md",
			MustContain: []string{
				"cloud-first runtime",
				"Secondary/local-only surfaces:",
				"does not own vendor-specific configurators, packaged plugins, or agent setup automation.",
				"project/workspace detection hints",
			},
			MustNotContain: commonForbidden,
		},
		{
			Path: "docs/INSTALLATION.md",
			MustContain: []string{
				"### Shared/cloud runtime",
				"lore mcp --tools=agent",
				"The TUI is a local convenience surface, not the primary hosted/admin workflow.",
				"compatibility stub",
				"## Railway preview setup",
				"Railway injects `PORT`; leave `LORE_PORT` unset unless you intentionally need to override the platform port.",
				"Set `LORE_ENV=staging`, `DATABASE_URL`, `LORE_BASE_URL`, and `LORE_JWT_SECRET` for the hosted preview contract.",
				"`/health` and `/mcp` smoke check",
				"Excluded scope: no web view/dashboard/browser UI work, no TUI work, no agent configurators/plugins, and no production auth or multi-user hardening.",
			},
			MustNotContain: commonForbidden,
		},
		{
			Path: ".env.example",
			MustContain: []string{
				"Railway preview profile",
				"LORE_ENV=staging",
				"# LORE_PORT=",
				"PORT=",
				"DATABASE_URL=",
				"LORE_BASE_URL=https://",
				"LORE_JWT_SECRET=",
				"Bind/base URL behavior",
			},
			MustNotContain: append(commonForbidden, "LORE_PORT=7437"),
		},
		{
			Path: "docs/COMPARISON.md",
			MustContain: []string{
				"Shared runtime + MCP + browser admin",
				"agents and external configurators consume stable Lore-owned primitives instead of repo-packaged vendor installers",
				"local convenience surfaces",
			},
			MustNotContain: commonForbidden,
		},
		{
			Path: "SECURITY.md",
			MustContain: []string{
				"shared/cloud runtime",
				"local SQLite mode remains a supported compatibility path",
				"`/mcp` exposure and tool authorization",
			},
			MustNotContain: commonForbidden,
		},
		{
			Path: "docs/AGENT-SETUP.md",
			MustContain: []string{
				"stable Lore-owned primitives",
				"external configurator or agent client",
				"mutate Claude/OpenCode/Gemini/Codex settings",
				"manual MCP/HTTP wiring",
			},
			MustNotContain: commonForbidden,
		},
		{
			Path: "docs/PLUGINS.md",
			MustContain: []string{
				"Lore does not ship or own vendor-specific plugin/configurator packages",
				"What Lore owns",
				"What external configurators own",
				"compatibility-only deprecation stub",
			},
			MustNotContain: commonForbidden,
		},
		{
			Path: "skills/architecture-guardrails/SKILL.md",
			MustContain: []string{
				"Cloud/runtime surfaces are the primary product boundary; local SQLite/TUI are compatibility surfaces.",
				"cloud-first story",
			},
			MustNotContain: []string{"local-first", "setup-first"},
		},
		{
			Path: "skills/backlog-triage/SKILL.md",
			MustContain: []string{
				"Cloud-first boundary",
				"Runtime over configurators",
				"break the cloud-first runtime boundary / single-binary / external-configurator ownership split",
			},
			MustNotContain: []string{"local-first", "setup-first"},
		},
	}
}

func retainedIntegrationPrimitives() []string {
	return []string{
		"lore serve",
		"lore mcp",
		"/mcp",
		"LORE_BASE_URL",
		"LORE_JWT_SECRET",
		"DATABASE_URL",
		"LORE_DATA_DIR",
		"LORE_PORT",
		"PORT",
		"LORE_PROJECT",
		"project/workspace detection hints",
	}
}

func docsContractFileSet() []string {
	contracts := cloudFirstDocsContract()
	files := make([]string, 0, len(contracts))
	for _, contract := range contracts {
		files = append(files, contract.Path)
	}
	return files
}

func removedSetupAssetReferences() []string {
	return []string{
		"plugin/opencode",
		"plugin/claude-code",
		".claude-plugin",
		"internal/setup/plugins",
		"opencode/plugins/lore.ts",
	}
}

func TestCloudFirstDocsContractPrimaryNarrative(t *testing.T) {
	root := repoRootFromCwd(t)

	for _, contract := range cloudFirstDocsContract() {
		content := mustReadFile(t, filepath.Join(root, contract.Path))

		for _, required := range contract.MustContain {
			if !strings.Contains(content, required) {
				t.Fatalf("%s must contain %q", contract.Path, required)
			}
		}

		for _, forbidden := range contract.MustNotContain {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s must not contain %q", contract.Path, forbidden)
			}
		}
	}
}

func TestCloudFirstDocsContractRetainedPrimitives(t *testing.T) {
	root := repoRootFromCwd(t)
	content := strings.Join([]string{
		mustReadFile(t, filepath.Join(root, "docs", "AGENT-SETUP.md")),
		mustReadFile(t, filepath.Join(root, "docs", "PLUGINS.md")),
		mustReadFile(t, filepath.Join(root, "docs", "ARCHITECTURE.md")),
	}, "\n")

	for _, primitive := range retainedIntegrationPrimitives() {
		if !strings.Contains(content, primitive) {
			t.Fatalf("retained primitive contract must mention %q", primitive)
		}
	}
}

func TestCloudFirstDocsContractRemovedAssetPathsStayGone(t *testing.T) {
	root := repoRootFromCwd(t)

	for _, rel := range docsContractFileSet() {
		content := mustReadFile(t, filepath.Join(root, rel))
		for _, forbidden := range removedSetupAssetReferences() {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s must not reference removed asset path %q", rel, forbidden)
			}
		}
	}
}
