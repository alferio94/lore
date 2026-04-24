package setup

import "fmt"

// Agent represents a legacy setup target kept only for compatibility messaging.
type Agent struct {
	Name        string
	Description string
	InstallDir  string
}

// Result is retained for compatibility with packages that still reference setup types.
type Result struct {
	Agent       string
	Destination string
	Files       int
}

var supportedAgents = []Agent{
	{Name: "opencode", Description: "Deprecated legacy setup target; use the external configurator", InstallDir: "external configurator"},
	{Name: "claude-code", Description: "Deprecated legacy setup target; use the external configurator", InstallDir: "external configurator"},
	{Name: "gemini-cli", Description: "Deprecated legacy setup target; use the external configurator", InstallDir: "external configurator"},
	{Name: "codex", Description: "Deprecated legacy setup target; use the external configurator", InstallDir: "external configurator"},
}

// SupportedAgents returns legacy agent names for compatibility surfaces that have
// not yet removed their setup UI.
func SupportedAgents() []Agent {
	agents := make([]Agent, len(supportedAgents))
	copy(agents, supportedAgents)
	return agents
}

// Install no longer performs vendor installation or filesystem writes.
func Install(agentName string) (*Result, error) {
	for _, agent := range supportedAgents {
		if agent.Name == agentName {
			return nil, deprecatedSetupError(agentName)
		}
	}
	return nil, fmt.Errorf("unknown agent: %q", agentName)
}

// AddClaudeCodeAllowlist no longer mutates Claude Code settings from Lore core.
func AddClaudeCodeAllowlist() error {
	return deprecatedSetupError("claude-code")
}

func deprecatedSetupError(agent string) error {
	return fmt.Errorf("setup for %s is deprecated; use the external configurator instead", agent)
}
