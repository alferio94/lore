package setup

import (
	"strings"
	"testing"
)

func TestSupportedAgentsListsLegacyCompatibilityTargets(t *testing.T) {
	agents := SupportedAgents()
	if len(agents) != 4 {
		t.Fatalf("expected 4 supported agents, got %d", len(agents))
	}

	want := []string{"opencode", "claude-code", "gemini-cli", "codex"}
	for i, name := range want {
		if agents[i].Name != name {
			t.Fatalf("agent %d = %q, want %q", i, agents[i].Name, name)
		}
		if !strings.Contains(strings.ToLower(agents[i].Description), "deprecated") {
			t.Fatalf("agent %q should be marked deprecated, got %q", agents[i].Name, agents[i].Description)
		}
	}
}

func TestInstallReturnsDeprecationErrorWithoutInstallerBehavior(t *testing.T) {
	tests := []string{"opencode", "claude-code", "gemini-cli", "codex"}
	for _, agent := range tests {
		t.Run(agent, func(t *testing.T) {
			result, err := Install(agent)
			if err == nil {
				t.Fatalf("expected deprecation error for %q", agent)
			}
			if result != nil {
				t.Fatalf("expected nil result for %q, got %#v", agent, result)
			}
			for _, expected := range []string{"deprecated", agent, "external configurator"} {
				if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(expected)) {
					t.Fatalf("error %q missing %q", err, expected)
				}
			}
		})
	}
}

func TestInstallRejectsUnknownAgent(t *testing.T) {
	_, err := Install("unknown")
	if err == nil || !strings.Contains(err.Error(), "unknown agent") {
		t.Fatalf("expected unknown agent error, got %v", err)
	}
}

func TestAddClaudeCodeAllowlistReturnsDeprecationError(t *testing.T) {
	err := AddClaudeCodeAllowlist()
	if err == nil {
		t.Fatal("expected deprecation error")
	}
	for _, expected := range []string{"deprecated", "claude-code", "external configurator"} {
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(expected)) {
			t.Fatalf("error %q missing %q", err, expected)
		}
	}
}
