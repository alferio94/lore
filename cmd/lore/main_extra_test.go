package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alferio94/lore/internal/admin"
	"github.com/alferio94/lore/internal/mcp"
	engramsrv "github.com/alferio94/lore/internal/server"
	"github.com/alferio94/lore/internal/setup"
	"github.com/alferio94/lore/internal/store"
	engramsync "github.com/alferio94/lore/internal/sync"
	"github.com/alferio94/lore/internal/tui"
	versioncheck "github.com/alferio94/lore/internal/version"

	tea "github.com/charmbracelet/bubbletea"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

type exitCode int

func captureOutputAndRecover(t *testing.T, fn func()) (stdout string, stderr string, recovered any) {
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

	func() {
		defer func() {
			recovered = recover()
		}()
		fn()
	}()

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

	return string(outBytes), string(errBytes), recovered
}

func stubExitWithPanic(t *testing.T) {
	t.Helper()
	old := exitFunc
	exitFunc = func(code int) { panic(exitCode(code)) }
	t.Cleanup(func() { exitFunc = old })
}

func stubRuntimeHooks(t *testing.T) {
	t.Helper()
	oldStoreNew := storeNew
	oldNewHTTPServer := newHTTPServer
	oldStartHTTP := startHTTP
	oldNewMCPServer := newMCPServer
	oldNewMCPServerWithTools := newMCPServerWithTools
	oldServeMCP := serveMCP
	oldNewTUIModel := newTUIModel
	oldNewTeaProgram := newTeaProgram
	oldRunTeaProgram := runTeaProgram
	oldSetupSupportedAgents := setupSupportedAgents
	oldSetupInstallAgent := setupInstallAgent
	oldScanInputLine := scanInputLine
	oldStoreSearch := storeSearch
	oldStoreAddObservation := storeAddObservation
	oldStoreTimeline := storeTimeline
	oldStoreFormatContext := storeFormatContext
	oldStoreStats := storeStats
	oldStoreExport := storeExport
	oldJSONMarshalIndent := jsonMarshalIndent
	oldSyncStatus := syncStatus
	oldSyncImport := syncImport
	oldSyncExport := syncExport
	oldCheckForUpdates := checkForUpdates
	oldMountAdmin := mountAdmin

	storeNew = store.New
	newHTTPServer = func(s *store.Store, cfg engramsrv.Config) *engramsrv.Server { return engramsrv.NewWithConfig(s, cfg) }
	startHTTP = func(_ *engramsrv.Server) error { return nil }
	newMCPServer = func(s *store.Store) *mcpserver.MCPServer {
		return mcpserver.NewMCPServer("test", "0", mcpserver.WithRecovery())
	}
	newMCPServerWithTools = func(s *store.Store, allowlist map[string]bool) *mcpserver.MCPServer {
		return mcpserver.NewMCPServer("test", "0", mcpserver.WithRecovery())
	}
	serveMCP = func(_ *mcpserver.MCPServer, _ ...mcpserver.StdioOption) error { return nil }
	newTUIModel = func(_ *store.Store) tui.Model { return tui.New(nil, "") }
	newTeaProgram = func(tea.Model, ...tea.ProgramOption) *tea.Program { return &tea.Program{} }
	runTeaProgram = func(*tea.Program) (tea.Model, error) { return nil, nil }
	setupSupportedAgents = setup.SupportedAgents
	setupInstallAgent = setup.Install
	scanInputLine = fmt.Scanln
	storeSearch = func(s *store.Store, query string, opts store.SearchOptions) ([]store.SearchResult, error) {
		return s.Search(query, opts)
	}
	storeAddObservation = func(s *store.Store, p store.AddObservationParams) (int64, error) {
		return s.AddObservation(p)
	}
	storeTimeline = func(s *store.Store, observationID int64, before, after int) (*store.TimelineResult, error) {
		return s.Timeline(observationID, before, after)
	}
	storeFormatContext = func(s *store.Store, project, scope string) (string, error) {
		return s.FormatContext(project, scope)
	}
	storeStats = func(s *store.Store) (*store.Stats, error) { return s.Stats() }
	storeExport = func(s *store.Store) (*store.ExportData, error) { return s.Export() }
	jsonMarshalIndent = json.MarshalIndent
	syncStatus = func(sy *engramsync.Syncer) (localChunks int, remoteChunks int, pendingImport int, err error) {
		return sy.Status()
	}
	syncImport = func(sy *engramsync.Syncer) (*engramsync.ImportResult, error) { return sy.Import() }
	syncExport = func(sy *engramsync.Syncer, createdBy, project string) (*engramsync.SyncResult, error) {
		return sy.Export(createdBy, project)
	}
	checkForUpdates = func(string) versioncheck.CheckResult {
		return versioncheck.CheckResult{Status: versioncheck.StatusUpToDate}
	}
	mountAdmin = admin.Mount

	t.Cleanup(func() {
		storeNew = oldStoreNew
		newHTTPServer = oldNewHTTPServer
		startHTTP = oldStartHTTP
		newMCPServer = oldNewMCPServer
		newMCPServerWithTools = oldNewMCPServerWithTools
		serveMCP = oldServeMCP
		newTUIModel = oldNewTUIModel
		newTeaProgram = oldNewTeaProgram
		runTeaProgram = oldRunTeaProgram
		setupSupportedAgents = oldSetupSupportedAgents
		setupInstallAgent = oldSetupInstallAgent
		scanInputLine = oldScanInputLine
		storeSearch = oldStoreSearch
		storeAddObservation = oldStoreAddObservation
		storeTimeline = oldStoreTimeline
		storeFormatContext = oldStoreFormatContext
		storeStats = oldStoreStats
		storeExport = oldStoreExport
		jsonMarshalIndent = oldJSONMarshalIndent
		syncStatus = oldSyncStatus
		syncImport = oldSyncImport
		syncExport = oldSyncExport
		checkForUpdates = oldCheckForUpdates
		mountAdmin = oldMountAdmin
	})
}

func TestFatal(t *testing.T) {
	stubExitWithPanic(t)
	_, stderr, recovered := captureOutputAndRecover(t, func() {
		fatal(errors.New("boom"))
	})

	code, ok := recovered.(exitCode)
	if !ok || int(code) != 1 {
		t.Fatalf("expected exit code 1 panic, got %v", recovered)
	}
	if !strings.Contains(stderr, "lore: boom") {
		t.Fatalf("fatal stderr mismatch: %q", stderr)
	}
}

func TestCmdServeParsesPortAndErrors(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)

	tests := []struct {
		name      string
		lorePort  string
		portEnv   string
		argPort   string
		wantPort  int
		startErr  error
		wantFatal bool
	}{
		{name: "default port", wantPort: 7437},
		{name: "PORT fallback", portEnv: "7000", wantPort: 7000},
		{name: "LORE_PORT takes precedence over PORT", lorePort: "8123", portEnv: "7000", wantPort: 8123},
		{name: "arg overrides env", lorePort: "8123", portEnv: "7000", argPort: "9001", wantPort: 9001},
		{name: "invalid LORE_PORT falls back to PORT", lorePort: "nope", portEnv: "7000", wantPort: 7000},
		{name: "invalid arg keeps env", lorePort: "8123", argPort: "bad", wantPort: 8123},
		{name: "invalid env keeps default", lorePort: "nope", portEnv: "also-bad", wantPort: 7437},
		{name: "start failure", wantPort: 7437, startErr: errors.New("listen failed"), wantFatal: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubExitWithPanic(t)
			if tc.lorePort != "" {
				t.Setenv("LORE_PORT", tc.lorePort)
			} else {
				t.Setenv("LORE_PORT", "")
			}
			if tc.portEnv != "" {
				t.Setenv("PORT", tc.portEnv)
			} else {
				t.Setenv("PORT", "")
			}

			args := []string{"lore", "serve"}
			if tc.argPort != "" {
				args = append(args, tc.argPort)
			}
			withArgs(t, args...)

			seenPort := -1
			newHTTPServer = func(s *store.Store, cfg engramsrv.Config) *engramsrv.Server {
				seenPort = cfg.Port
				return engramsrv.NewWithConfig(s, cfg)
			}
			startHTTP = func(_ *engramsrv.Server) error {
				return tc.startErr
			}

			_, stderr, recovered := captureOutputAndRecover(t, func() {
				cmdServe(cfg)
			})

			if seenPort != tc.wantPort {
				t.Fatalf("port=%d want=%d", seenPort, tc.wantPort)
			}
			if tc.wantFatal {
				if _, ok := recovered.(exitCode); !ok {
					t.Fatalf("expected fatal exit, got %v", recovered)
				}
				if !strings.Contains(stderr, "listen failed") {
					t.Fatalf("stderr missing start error: %q", stderr)
				}
			} else if recovered != nil {
				t.Fatalf("expected no panic, got %v", recovered)
			}
		})
	}
}

func TestCmdMCPAndTUIBranches(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	serveMCP = func(_ *mcpserver.MCPServer, _ ...mcpserver.StdioOption) error { return errors.New("mcp failed") }
	_, mcpErr, recovered := captureOutputAndRecover(t, func() { cmdMCP(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(mcpErr, "mcp failed") {
		t.Fatalf("expected mcp fatal, got panic=%v stderr=%q", recovered, mcpErr)
	}

	serveMCP = func(_ *mcpserver.MCPServer, _ ...mcpserver.StdioOption) error { return nil }
	_, _, recovered = captureOutputAndRecover(t, func() { cmdMCP(cfg) })
	if recovered != nil {
		t.Fatalf("unexpected panic on successful mcp: %v", recovered)
	}

	runTeaProgram = func(*tea.Program) (tea.Model, error) { return nil, errors.New("tui failed") }
	_, tuiErr, recovered := captureOutputAndRecover(t, func() { cmdTUI(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(tuiErr, "tui failed") {
		t.Fatalf("expected tui fatal, got panic=%v stderr=%q", recovered, tuiErr)
	}

	runTeaProgram = func(*tea.Program) (tea.Model, error) { return nil, nil }
	_, _, recovered = captureOutputAndRecover(t, func() { cmdTUI(cfg) })
	if recovered != nil {
		t.Fatalf("unexpected panic on successful tui: %v", recovered)
	}
}

func TestCmdSetupDirectAndInteractive(t *testing.T) {
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	setupInstallAgent = func(agent string) (*setup.Result, error) {
		if agent == "broken" {
			return nil, errors.New("install failed")
		}
		return &setup.Result{Agent: agent, Destination: "/tmp/dest", Files: 2}, nil
	}

	withArgs(t, "lore", "setup", "codex")
	out, errOut, recovered := captureOutputAndRecover(t, func() { cmdSetup() })
	if recovered != nil || errOut != "" {
		t.Fatalf("direct setup should succeed, panic=%v stderr=%q", recovered, errOut)
	}
	if !strings.Contains(out, "Installed codex plugin") {
		t.Fatalf("unexpected direct setup output: %q", out)
	}

	withArgs(t, "lore", "setup", "broken")
	_, errOut, recovered = captureOutputAndRecover(t, func() { cmdSetup() })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(errOut, "install failed") {
		t.Fatalf("expected direct setup fatal, panic=%v stderr=%q", recovered, errOut)
	}

	setupSupportedAgents = func() []setup.Agent {
		return []setup.Agent{{Name: "opencode", Description: "OpenCode", InstallDir: "/tmp/opencode"}}
	}
	scanInputLine = func(a ...any) (int, error) {
		p := a[0].(*string)
		*p = "1"
		return 1, nil
	}

	withArgs(t, "lore", "setup")
	out, errOut, recovered = captureOutputAndRecover(t, func() { cmdSetup() })
	if recovered != nil || errOut != "" {
		t.Fatalf("interactive setup should succeed, panic=%v stderr=%q", recovered, errOut)
	}
	if !strings.Contains(out, "Installing opencode plugin") {
		t.Fatalf("unexpected interactive setup output: %q", out)
	}

	scanInputLine = func(a ...any) (int, error) {
		p := a[0].(*string)
		*p = "99"
		return 1, nil
	}
	withArgs(t, "lore", "setup")
	_, errOut, recovered = captureOutputAndRecover(t, func() { cmdSetup() })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(errOut, "Invalid choice") {
		t.Fatalf("expected invalid choice exit, panic=%v stderr=%q", recovered, errOut)
	}
}

func TestCmdExportDefaultAndCmdImportErrors(t *testing.T) {
	workDir := t.TempDir()
	withCwd(t, workDir)

	cfg := testConfig(t)
	stubExitWithPanic(t)

	mustSeedObservation(t, cfg, "s-exp-default", "proj", "note", "title", "content", "project")

	withArgs(t, "lore", "export")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdExport(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("export default should succeed, panic=%v stderr=%q", recovered, stderr)
	}
	if !strings.Contains(stdout, "Exported to lore-export.json") {
		t.Fatalf("unexpected default export output: %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(workDir, "lore-export.json")); err != nil {
		t.Fatalf("expected default export file: %v", err)
	}

	badPath := filepath.Join(workDir, "missing", "out.json")
	withArgs(t, "lore", "export", badPath)
	_, stderr, recovered = captureOutputAndRecover(t, func() { cmdExport(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(stderr, "no such file or directory") {
		t.Fatalf("expected export write fatal, panic=%v stderr=%q", recovered, stderr)
	}

	withArgs(t, "lore", "import")
	_, stderr, recovered = captureOutputAndRecover(t, func() { cmdImport(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(stderr, "usage: lore import") {
		t.Fatalf("expected import usage exit, panic=%v stderr=%q", recovered, stderr)
	}

	withArgs(t, "lore", "import", filepath.Join(workDir, "nope.json"))
	_, stderr, recovered = captureOutputAndRecover(t, func() { cmdImport(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(stderr, "read") {
		t.Fatalf("expected import read fatal, panic=%v stderr=%q", recovered, stderr)
	}

	invalidJSON := filepath.Join(workDir, "invalid.json")
	if err := os.WriteFile(invalidJSON, []byte("{invalid"), 0644); err != nil {
		t.Fatalf("write invalid json: %v", err)
	}
	withArgs(t, "lore", "import", invalidJSON)
	_, stderr, recovered = captureOutputAndRecover(t, func() { cmdImport(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(stderr, "parse") {
		t.Fatalf("expected import parse fatal, panic=%v stderr=%q", recovered, stderr)
	}
}

func TestMainDispatchServeMCPAndTUI(t *testing.T) {
	stubRuntimeHooks(t)

	t.Setenv("LORE_DATA_DIR", t.TempDir())
	withArgs(t, "lore", "serve", "8088")
	_, stderr, recovered := captureOutputAndRecover(t, func() { main() })
	if recovered != nil || stderr != "" {
		t.Fatalf("serve dispatch failed: panic=%v stderr=%q", recovered, stderr)
	}

	withArgs(t, "lore", "mcp")
	_, stderr, recovered = captureOutputAndRecover(t, func() { main() })
	if recovered != nil || stderr != "" {
		t.Fatalf("mcp dispatch failed: panic=%v stderr=%q", recovered, stderr)
	}

	withArgs(t, "lore", "tui")
	_, stderr, recovered = captureOutputAndRecover(t, func() { main() })
	if recovered != nil || stderr != "" {
		t.Fatalf("tui dispatch failed: panic=%v stderr=%q", recovered, stderr)
	}
}

func TestStoreInitFailurePaths(t *testing.T) {
	stubRuntimeHooks(t)
	stubExitWithPanic(t)
	cfg := testConfig(t)
	importFile := filepath.Join(t.TempDir(), "import.json")
	if err := os.WriteFile(importFile, []byte(`{"version":"0.1.0","exported_at":"2026-01-01T00:00:00Z","sessions":[],"observations":[],"prompts":[]}`), 0644); err != nil {
		t.Fatalf("write import file: %v", err)
	}

	storeNew = func(store.Config) (*store.Store, error) {
		return nil, errors.New("store init failed")
	}

	cmds := []func(store.Config){
		cmdServe,
		cmdMCP,
		cmdTUI,
		cmdSearch,
		cmdSave,
		cmdTimeline,
		cmdContext,
		cmdStats,
		cmdExport,
		cmdImport,
		cmdSync,
	}

	argsByCmd := [][]string{
		{"lore", "serve"},
		{"lore", "mcp"},
		{"lore", "tui"},
		{"lore", "search", "q"},
		{"lore", "save", "t", "c"},
		{"lore", "timeline", "1"},
		{"lore", "context"},
		{"lore", "stats"},
		{"lore", "export"},
		{"lore", "import", importFile},
		{"lore", "sync"},
	}

	for i, fn := range cmds {
		withArgs(t, argsByCmd[i]...)
		_, stderr, recovered := captureOutputAndRecover(t, func() { fn(cfg) })
		if _, ok := recovered.(exitCode); !ok {
			t.Fatalf("command %d: expected exit panic, got %v", i, recovered)
		}
		if !strings.Contains(stderr, "store init failed") {
			t.Fatalf("command %d: expected store failure stderr, got %q", i, stderr)
		}
	}
}

func TestUsageAndValidationExits(t *testing.T) {
	cfg := testConfig(t)
	stubExitWithPanic(t)

	tests := []struct {
		name       string
		args       []string
		run        func(store.Config)
		errSubstr  string
		stderrOnly bool
	}{
		{name: "search usage", args: []string{"lore", "search"}, run: cmdSearch, errSubstr: "usage: lore search"},
		{name: "search missing query", args: []string{"lore", "search", "--limit", "3"}, run: cmdSearch, errSubstr: "search query is required"},
		{name: "save usage", args: []string{"lore", "save", "title"}, run: cmdSave, errSubstr: "usage: lore save"},
		{name: "timeline usage", args: []string{"lore", "timeline"}, run: cmdTimeline, errSubstr: "usage: lore timeline"},
		{name: "timeline invalid id", args: []string{"lore", "timeline", "abc"}, run: cmdTimeline, errSubstr: "invalid observation id"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withArgs(t, tc.args...)
			_, stderr, recovered := captureOutputAndRecover(t, func() { tc.run(cfg) })
			if _, ok := recovered.(exitCode); !ok {
				t.Fatalf("expected exit panic, got %v", recovered)
			}
			if !strings.Contains(stderr, tc.errSubstr) {
				t.Fatalf("stderr missing %q: %q", tc.errSubstr, stderr)
			}
		})
	}
}

func TestMainDispatchRemainingCommands(t *testing.T) {
	stubRuntimeHooks(t)
	stubExitWithPanic(t)
	withCwd(t, t.TempDir())

	dataDir := t.TempDir()
	t.Setenv("LORE_DATA_DIR", dataDir)

	seedCfg, scErr := store.DefaultConfig()
	if scErr != nil {
		t.Fatalf("DefaultConfig: %v", scErr)
	}
	seedCfg.DataDir = dataDir
	focusID := mustSeedObservation(t, seedCfg, "s-main", "main-proj", "note", "focus", "focus content", "project")

	importFile := filepath.Join(t.TempDir(), "import.json")
	if err := os.WriteFile(importFile, []byte(`{"version":"0.1.0","exported_at":"2026-01-01T00:00:00Z","sessions":[],"observations":[],"prompts":[]}`), 0644); err != nil {
		t.Fatalf("write import file: %v", err)
	}

	setupInstallAgent = func(agent string) (*setup.Result, error) {
		return &setup.Result{Agent: agent, Destination: "/tmp/dest", Files: 1}, nil
	}

	tests := []struct {
		name string
		args []string
	}{
		{name: "search", args: []string{"lore", "search", "focus"}},
		{name: "save", args: []string{"lore", "save", "t", "c"}},
		{name: "timeline", args: []string{"lore", "timeline", fmt.Sprintf("%d", focusID)}},
		{name: "context", args: []string{"lore", "context", "main-proj"}},
		{name: "stats", args: []string{"lore", "stats"}},
		{name: "export", args: []string{"lore", "export", filepath.Join(t.TempDir(), "exp.json")}},
		{name: "import", args: []string{"lore", "import", importFile}},
		{name: "sync", args: []string{"lore", "sync", "--all"}},
		{name: "setup", args: []string{"lore", "setup", "codex"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withArgs(t, tc.args...)
			_, stderr, recovered := captureOutputAndRecover(t, func() { main() })
			if recovered != nil {
				t.Fatalf("main panic for %s: %v stderr=%q", tc.name, recovered, stderr)
			}
		})
	}
}

func TestCmdSyncAdditionalBranches(t *testing.T) {
	stubExitWithPanic(t)

	t.Run("all projects empty export message", func(t *testing.T) {
		workDir := t.TempDir()
		withCwd(t, workDir)
		cfg := testConfig(t)

		withArgs(t, "lore", "sync", "--all")
		stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdSync(cfg) })
		if recovered != nil || stderr != "" {
			t.Fatalf("expected clean run, panic=%v stderr=%q", recovered, stderr)
		}
		if !strings.Contains(stdout, "Exporting ALL memories") || !strings.Contains(stdout, "Nothing new to sync") {
			t.Fatalf("unexpected output: %q", stdout)
		}
	})

	t.Run("status parse error", func(t *testing.T) {
		workDir := t.TempDir()
		withCwd(t, workDir)
		cfg := testConfig(t)

		if err := os.MkdirAll(filepath.Join(workDir, ".lore"), 0755); err != nil {
			t.Fatalf("mkdir .lore: %v", err)
		}
		if err := os.WriteFile(filepath.Join(workDir, ".lore", "manifest.json"), []byte("{bad json"), 0644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		withArgs(t, "lore", "sync", "--status")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdSync(cfg) })
		if _, ok := recovered.(exitCode); !ok {
			t.Fatalf("expected fatal exit, got %v", recovered)
		}
		if !strings.Contains(stderr, "parse manifest") {
			t.Fatalf("unexpected stderr: %q", stderr)
		}
	})

	t.Run("import parse error", func(t *testing.T) {
		workDir := t.TempDir()
		withCwd(t, workDir)
		cfg := testConfig(t)

		if err := os.MkdirAll(filepath.Join(workDir, ".lore"), 0755); err != nil {
			t.Fatalf("mkdir .lore: %v", err)
		}
		if err := os.WriteFile(filepath.Join(workDir, ".lore", "manifest.json"), []byte("{bad json"), 0644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		withArgs(t, "lore", "sync", "--import")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdSync(cfg) })
		if _, ok := recovered.(exitCode); !ok {
			t.Fatalf("expected fatal exit, got %v", recovered)
		}
		if !strings.Contains(stderr, "parse manifest") {
			t.Fatalf("unexpected stderr: %q", stderr)
		}
	})

	t.Run("export parse error", func(t *testing.T) {
		workDir := t.TempDir()
		withCwd(t, workDir)
		cfg := testConfig(t)

		if err := os.MkdirAll(filepath.Join(workDir, ".lore"), 0755); err != nil {
			t.Fatalf("mkdir .lore: %v", err)
		}
		if err := os.WriteFile(filepath.Join(workDir, ".lore", "manifest.json"), []byte("{bad json"), 0644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		withArgs(t, "lore", "sync")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdSync(cfg) })
		if _, ok := recovered.(exitCode); !ok {
			t.Fatalf("expected fatal exit, got %v", recovered)
		}
		if !strings.Contains(stderr, "parse manifest") {
			t.Fatalf("unexpected stderr: %q", stderr)
		}
	})
}

func TestCmdImportStoreImportFailure(t *testing.T) {
	stubExitWithPanic(t)
	cfg := testConfig(t)

	badImport := filepath.Join(t.TempDir(), "bad-import.json")
	badJSON := `{
		"version":"0.1.0",
		"exported_at":"2026-01-01T00:00:00Z",
		"sessions":[],
		"observations":[{"id":1,"session_id":"missing-session","type":"note","title":"x","content":"y","scope":"project","revision_count":1,"duplicate_count":1,"created_at":"2026-01-01 00:00:00","updated_at":"2026-01-01 00:00:00"}],
		"prompts":[]
	}`
	if err := os.WriteFile(badImport, []byte(badJSON), 0644); err != nil {
		t.Fatalf("write bad import: %v", err)
	}

	withArgs(t, "lore", "import", badImport)
	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdImport(cfg) })
	if _, ok := recovered.(exitCode); !ok {
		t.Fatalf("expected fatal exit, got %v", recovered)
	}
	if !strings.Contains(stderr, "import observation") {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
}

func TestCmdSearchAndSaveDanglingFlags(t *testing.T) {
	cfg := testConfig(t)

	withArgs(t, "lore", "save", "dangling-title", "dangling-content", "--type")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdSave(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("save with dangling flag failed, panic=%v stderr=%q", recovered, stderr)
	}
	if !strings.Contains(stdout, "Memory saved:") {
		t.Fatalf("unexpected save output: %q", stdout)
	}

	withArgs(t, "lore", "search", "dangling-content", "--limit", "not-a-number", "--project")
	stdout, stderr, recovered = captureOutputAndRecover(t, func() { cmdSearch(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("search with dangling flags failed, panic=%v stderr=%q", recovered, stderr)
	}
	if !strings.Contains(stdout, "Found") {
		t.Fatalf("unexpected search output: %q", stdout)
	}
}

func TestCmdSetupHyphenArgFallsBackToInteractive(t *testing.T) {
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	setupSupportedAgents = func() []setup.Agent {
		return []setup.Agent{{Name: "codex", Description: "Codex", InstallDir: "/tmp/codex"}}
	}
	setupInstallAgent = func(agent string) (*setup.Result, error) {
		return &setup.Result{Agent: agent, Destination: "/tmp/codex", Files: 1}, nil
	}
	scanInputLine = func(a ...any) (int, error) {
		p := a[0].(*string)
		*p = "1"
		return 1, nil
	}

	withArgs(t, "lore", "setup", "--not-an-agent")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdSetup() })
	if recovered != nil || stderr != "" {
		t.Fatalf("setup interactive fallback failed: panic=%v stderr=%q", recovered, stderr)
	}
	if !strings.Contains(stdout, "Which agent do you want to set up?") || !strings.Contains(stdout, "Installing codex plugin") {
		t.Fatalf("unexpected setup output: %q", stdout)
	}
}

func TestCmdTimelineNoBeforeAfterSections(t *testing.T) {
	cfg := testConfig(t)
	focusID := mustSeedObservation(t, cfg, "solo-session", "solo", "note", "focus", "only content", "project")

	withArgs(t, "lore", "timeline", fmt.Sprintf("%d", focusID), "--before", "0", "--after", "0")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdTimeline(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("timeline failed: panic=%v stderr=%q", recovered, stderr)
	}
	if strings.Contains(stdout, "─── Before ───") || strings.Contains(stdout, "─── After ───") {
		t.Fatalf("unexpected before/after sections in output: %q", stdout)
	}
}

func TestCmdStatsNoProjectsYet(t *testing.T) {
	cfg := testConfig(t)
	withArgs(t, "lore", "stats")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdStats(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("stats failed: panic=%v stderr=%q", recovered, stderr)
	}
	if !strings.Contains(stdout, "Projects:     none yet") {
		t.Fatalf("expected empty projects output, got: %q", stdout)
	}
}

func TestCmdSyncImportEmptyAndMixedChunks(t *testing.T) {
	stubExitWithPanic(t)

	t.Run("import with empty manifest", func(t *testing.T) {
		workDir := t.TempDir()
		withCwd(t, workDir)
		cfg := testConfig(t)

		if err := os.MkdirAll(filepath.Join(workDir, ".lore"), 0755); err != nil {
			t.Fatalf("mkdir .lore: %v", err)
		}
		if err := os.WriteFile(filepath.Join(workDir, ".lore", "manifest.json"), []byte(`{"version":1,"chunks":[]}`), 0644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}

		withArgs(t, "lore", "sync", "--import")
		stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdSync(cfg) })
		if recovered != nil || stderr != "" {
			t.Fatalf("empty import failed: panic=%v stderr=%q", recovered, stderr)
		}
		if !strings.Contains(stdout, "No new chunks to import") || strings.Contains(stdout, "already imported") {
			t.Fatalf("unexpected empty import output: %q", stdout)
		}
	})

	t.Run("import new plus skipped chunk", func(t *testing.T) {
		workDir := t.TempDir()
		withCwd(t, workDir)

		exportCfg := testConfig(t)
		importCfg := testConfig(t)

		mustSeedObservation(t, exportCfg, "mix-1", "mix", "note", "one", "content-one", "project")
		withArgs(t, "lore", "sync", "--all")
		_, _, _ = captureOutputAndRecover(t, func() { cmdSync(exportCfg) })

		withArgs(t, "lore", "sync", "--import")
		_, _, _ = captureOutputAndRecover(t, func() { cmdSync(importCfg) })

		time.Sleep(1100 * time.Millisecond)
		mustSeedObservation(t, exportCfg, "mix-2", "mix", "note", "two", "content-two", "project")
		withArgs(t, "lore", "sync", "--all")
		_, _, _ = captureOutputAndRecover(t, func() { cmdSync(exportCfg) })

		withArgs(t, "lore", "sync", "--import")
		stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdSync(importCfg) })
		if recovered != nil || stderr != "" {
			t.Fatalf("mixed import failed: panic=%v stderr=%q", recovered, stderr)
		}
		if !strings.Contains(stdout, "Imported 1 new chunk(s)") || !strings.Contains(stdout, "Skipped:") {
			t.Fatalf("unexpected mixed import output: %q", stdout)
		}
	})
}

func TestCommandErrorSeamsAndUncoveredBranches(t *testing.T) {
	stubRuntimeHooks(t)
	stubExitWithPanic(t)
	cfg := testConfig(t)

	assertFatal := func(t *testing.T, stderr string, recovered any, want string) {
		t.Helper()
		if _, ok := recovered.(exitCode); !ok {
			t.Fatalf("expected fatal exit, got %v", recovered)
		}
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q: %q", want, stderr)
		}
	}

	t.Run("search seam error", func(t *testing.T) {
		withArgs(t, "lore", "search", "needle")
		storeSearch = func(*store.Store, string, store.SearchOptions) ([]store.SearchResult, error) {
			return nil, errors.New("forced search error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdSearch(cfg) })
		assertFatal(t, stderr, recovered, "forced search error")
	})

	t.Run("save seam error", func(t *testing.T) {
		withArgs(t, "lore", "save", "title", "content")
		storeAddObservation = func(*store.Store, store.AddObservationParams) (int64, error) {
			return 0, errors.New("forced save error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdSave(cfg) })
		assertFatal(t, stderr, recovered, "forced save error")
	})

	t.Run("timeline seam error", func(t *testing.T) {
		withArgs(t, "lore", "timeline", "1")
		storeTimeline = func(*store.Store, int64, int, int) (*store.TimelineResult, error) {
			return nil, errors.New("forced timeline error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdTimeline(cfg) })
		assertFatal(t, stderr, recovered, "forced timeline error")
	})

	t.Run("timeline prints session summary", func(t *testing.T) {
		summary := "this session has a non-empty summary"
		withArgs(t, "lore", "timeline", "1")
		storeTimeline = func(*store.Store, int64, int, int) (*store.TimelineResult, error) {
			return &store.TimelineResult{
				Focus:        store.Observation{ID: 1, Type: "note", Title: "focus", Content: "content", CreatedAt: "2026-01-01"},
				SessionInfo:  &store.Session{Project: "proj", StartedAt: "2026-01-01", Summary: &summary},
				TotalInRange: 1,
			}, nil
		}
		stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdTimeline(cfg) })
		if recovered != nil || stderr != "" {
			t.Fatalf("expected successful timeline render, panic=%v stderr=%q", recovered, stderr)
		}
		if !strings.Contains(stdout, "Session: proj") || !strings.Contains(stdout, "non-empty summary") {
			t.Fatalf("expected summary in timeline output, got: %q", stdout)
		}
	})

	t.Run("context seam error", func(t *testing.T) {
		withArgs(t, "lore", "context")
		storeFormatContext = func(*store.Store, string, string) (string, error) {
			return "", errors.New("forced context error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdContext(cfg) })
		assertFatal(t, stderr, recovered, "forced context error")
	})

	t.Run("stats seam error", func(t *testing.T) {
		withArgs(t, "lore", "stats")
		storeStats = func(*store.Store) (*store.Stats, error) {
			return nil, errors.New("forced stats error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdStats(cfg) })
		assertFatal(t, stderr, recovered, "forced stats error")
	})

	t.Run("export seam error", func(t *testing.T) {
		withArgs(t, "lore", "export")
		storeExport = func(*store.Store) (*store.ExportData, error) {
			return nil, errors.New("forced export error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdExport(cfg) })
		assertFatal(t, stderr, recovered, "forced export error")
	})

	t.Run("export marshal seam error", func(t *testing.T) {
		withArgs(t, "lore", "export")
		storeExport = func(s *store.Store) (*store.ExportData, error) { return s.Export() }
		jsonMarshalIndent = func(any, string, string) ([]byte, error) {
			return nil, errors.New("forced marshal error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdExport(cfg) })
		assertFatal(t, stderr, recovered, "forced marshal error")
	})

	t.Run("sync seam status error", func(t *testing.T) {
		withCwd(t, t.TempDir())
		withArgs(t, "lore", "sync", "--status")
		syncStatus = func(*engramsync.Syncer) (int, int, int, error) {
			return 0, 0, 0, errors.New("forced status error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdSync(cfg) })
		assertFatal(t, stderr, recovered, "forced status error")
	})

	t.Run("sync uses explicit project flag", func(t *testing.T) {
		withCwd(t, t.TempDir())
		withArgs(t, "lore", "sync", "--project", "explicit-proj")
		stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdSync(cfg) })
		if recovered != nil || stderr != "" {
			t.Fatalf("sync with --project should succeed, panic=%v stderr=%q", recovered, stderr)
		}
		if !strings.Contains(stdout, `Exporting memories for project "explicit-proj"`) {
			t.Fatalf("expected explicit project output, got: %q", stdout)
		}
	})

	t.Run("setup interactive install error", func(t *testing.T) {
		setupSupportedAgents = func() []setup.Agent {
			return []setup.Agent{{Name: "codex", Description: "Codex", InstallDir: "/tmp/codex"}}
		}
		scanInputLine = func(a ...any) (int, error) {
			p := a[0].(*string)
			*p = "1"
			return 1, nil
		}
		setupInstallAgent = func(string) (*setup.Result, error) {
			return nil, errors.New("forced setup error")
		}

		withArgs(t, "lore", "setup")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdSetup() })
		assertFatal(t, stderr, recovered, "forced setup error")
	})
}

// ─── Task 3.3: cmdServe MCP wiring test ──────────────────────────────────────

// TestCmdServeWiresMCPHandler verifies that when cmdServe runs:
//  1. newMCPHTTPHandler is called (the injectable var from task 1.5) with a
//     non-nil store and a string project hint.
//  2. SetMCPHandler is called on the HTTP server — verified by sending a request
//     to /mcp on the captured server and asserting it does NOT return 404.
func TestCmdServeWiresMCPHandler(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	// Capture the handler provided to newMCPHTTPHandler.
	var mcpHandlerCalled bool
	var capturedSrv *engramsrv.Server

	oldMCPHTTPHandler := newMCPHTTPHandler
	t.Cleanup(func() { newMCPHTTPHandler = oldMCPHTTPHandler })
	// Inject a real stub handler that returns 200 OK for any request.
	newMCPHTTPHandler = func(s *store.Store, projectHint string) http.Handler {
		mcpHandlerCalled = true
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	}

	// Capture the server returned by newHTTPServer so we can query it later.
	oldNewHTTPServer := newHTTPServer
	t.Cleanup(func() { newHTTPServer = oldNewHTTPServer })
	newHTTPServer = func(s *store.Store, cfg engramsrv.Config) *engramsrv.Server {
		srv := engramsrv.NewWithConfig(s, cfg)
		capturedSrv = srv
		return srv
	}

	// Prevent actual HTTP listen; return immediately.
	startHTTP = func(_ *engramsrv.Server) error { return nil }

	withArgs(t, "lore", "serve")
	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdServe(cfg) })
	if recovered != nil {
		t.Fatalf("expected clean run, got panic=%v stderr=%q", recovered, stderr)
	}

	// Assertion 1: newMCPHTTPHandler was called during cmdServe.
	if !mcpHandlerCalled {
		t.Fatal("expected newMCPHTTPHandler to be called during cmdServe, but it was not")
	}

	// Assertion 2: the server's /mcp route responds (not 404).
	if capturedSrv == nil {
		t.Fatal("expected newHTTPServer to be called and server captured")
	}
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rec := httptest.NewRecorder()
	capturedSrv.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("expected /mcp to be mounted (not 404), got %d", rec.Code)
	}
}

// TestCmdServeUsesProjectHintFromEnv verifies that the LORE_PROJECT env var
// is passed to newMCPHTTPHandler as the projectHint.
func TestCmdServeUsesProjectHintFromEnv(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	t.Setenv("LORE_PROJECT", "my-env-project")

	var capturedHint string
	oldMCPHTTPHandler := newMCPHTTPHandler
	t.Cleanup(func() { newMCPHTTPHandler = oldMCPHTTPHandler })
	newMCPHTTPHandler = func(s *store.Store, projectHint string) http.Handler {
		capturedHint = projectHint
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	}
	startHTTP = func(_ *engramsrv.Server) error { return nil }

	withArgs(t, "lore", "serve")
	_, _, recovered := captureOutputAndRecover(t, func() { cmdServe(cfg) })
	if recovered != nil {
		t.Fatalf("unexpected panic: %v", recovered)
	}

	if capturedHint != "my-env-project" {
		t.Fatalf("expected projectHint=%q, got %q", "my-env-project", capturedHint)
	}
}

// TestCmdServeRuntimeContractIntegrationLike verifies runtime/storage wiring
// together with mounted HTTP contracts in one end-to-end-ish cmdServe pass.
func TestCmdServeRuntimeContractIntegrationLike(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)

	storageDir := t.TempDir()
	t.Setenv("LORE_ENV", "local")
	t.Setenv("LORE_HOST", "127.0.0.1")
	t.Setenv("LORE_PORT", "")
	t.Setenv("PORT", "7666")
	t.Setenv("LORE_DATA_DIR", storageDir)
	t.Setenv("DATABASE_URL", "postgres://lore:secret@db.internal:5432/lore")
	t.Setenv("LORE_PROJECT", "Runtime-Project")
	withArgs(t, "lore", "serve", "--dev-auth")

	seenStoreDataDir := ""
	seenStoreDatabaseURL := ""
	seenServerHost := ""
	seenServerPort := 0
	seenProjectHint := ""
	adminMounted := false

	storeNew = func(in store.Config) (*store.Store, error) {
		seenStoreDataDir = in.DataDir
		seenStoreDatabaseURL = in.DatabaseURL
		return store.New(in)
	}
	newHTTPServer = func(s *store.Store, cfg engramsrv.Config) *engramsrv.Server {
		seenServerHost = cfg.Host
		seenServerPort = cfg.Port
		return engramsrv.NewWithConfig(s, cfg)
	}
	newMCPHTTPHandler = func(_ *store.Store, projectHint string) http.Handler {
		seenProjectHint = projectHint
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	}
	mountAdmin = func(mux *http.ServeMux, cfg admin.AdminConfig) {
		adminMounted = true
		if !cfg.DevAuth {
			t.Fatalf("expected DevAuth=true when --dev-auth flag is present")
		}
		admin.Mount(mux, cfg)
	}

	startHTTP = func(srv *engramsrv.Server) error {
		mcpReq := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		mcpRec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(mcpRec, mcpReq)
		if mcpRec.Code == http.StatusNotFound {
			return fmt.Errorf("expected /mcp route to be mounted")
		}

		adminReq := httptest.NewRequest(http.MethodGet, "/admin/", nil)
		adminRec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(adminRec, adminReq)
		if adminRec.Code == http.StatusNotFound {
			return fmt.Errorf("expected /admin route to be mounted")
		}

		return nil
	}

	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdServe(cfg) })
	if recovered != nil {
		t.Fatalf("expected clean cmdServe run, got panic=%v stderr=%q", recovered, stderr)
	}

	if seenServerHost != "127.0.0.1" {
		t.Fatalf("server host = %q, want 127.0.0.1", seenServerHost)
	}
	if seenServerPort != 7666 {
		t.Fatalf("server port = %d, want 7666 from PORT fallback", seenServerPort)
	}
	if seenStoreDataDir != storageDir {
		t.Fatalf("store DataDir = %q, want %q", seenStoreDataDir, storageDir)
	}
	if seenStoreDatabaseURL == "" {
		t.Fatalf("expected store config to retain DATABASE_URL")
	}
	if seenProjectHint != "runtime-project" {
		t.Fatalf("project hint = %q, want normalized runtime-project", seenProjectHint)
	}
	if !adminMounted {
		t.Fatalf("expected admin routes to be mounted")
	}
}

func TestCmdMCP(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	assertFatal := func(t *testing.T, stderr string, recovered any, want string) {
		t.Helper()
		code, ok := recovered.(exitCode)
		if !ok || int(code) != 1 {
			t.Fatalf("expected exit code 1 panic, got %v", recovered)
		}
		if !strings.Contains(stderr, want) {
			t.Fatalf("expected stderr to contain %q, got %q", want, stderr)
		}
	}

	t.Run("no tools filter uses newMCPServerWithConfig with nil allowlist", func(t *testing.T) {
		called := false
		newMCPServerWithConfig = func(s *store.Store, mcpCfg mcp.MCPConfig, allowlist map[string]bool) *mcpserver.MCPServer {
			called = true
			if allowlist != nil {
				t.Errorf("expected nil allowlist for no tools filter, got %v", allowlist)
			}
			return mcpserver.NewMCPServer("test", "0")
		}
		withArgs(t, "lore", "mcp")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdMCP(cfg) })
		if recovered != nil || stderr != "" {
			t.Fatalf("expected clean run, got panic=%v stderr=%q", recovered, stderr)
		}
		if !called {
			t.Fatal("expected newMCPServerWithConfig to be called")
		}
	})

	t.Run("--tools flag uses newMCPServerWithConfig with non-nil allowlist", func(t *testing.T) {
		var gotAllowlist map[string]bool
		newMCPServerWithConfig = func(s *store.Store, mcpCfg mcp.MCPConfig, allowlist map[string]bool) *mcpserver.MCPServer {
			gotAllowlist = allowlist
			return mcpserver.NewMCPServer("test", "0")
		}
		withArgs(t, "lore", "mcp", "--tools=agent")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdMCP(cfg) })
		if recovered != nil || stderr != "" {
			t.Fatalf("expected clean run, got panic=%v stderr=%q", recovered, stderr)
		}
		if gotAllowlist == nil {
			t.Fatal("expected newMCPServerWithConfig to be called with non-nil allowlist")
		}
	})

	t.Run("--tools as separate arg uses newMCPServerWithConfig with non-nil allowlist", func(t *testing.T) {
		var gotAllowlist map[string]bool
		newMCPServerWithConfig = func(s *store.Store, mcpCfg mcp.MCPConfig, allowlist map[string]bool) *mcpserver.MCPServer {
			gotAllowlist = allowlist
			return mcpserver.NewMCPServer("test", "0")
		}
		withArgs(t, "lore", "mcp", "--tools", "agent")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdMCP(cfg) })
		if recovered != nil || stderr != "" {
			t.Fatalf("expected clean run, got panic=%v stderr=%q", recovered, stderr)
		}
		if gotAllowlist == nil {
			t.Fatal("expected newMCPServerWithConfig to be called with non-nil allowlist")
		}
	})

	t.Run("storeNew failure calls fatal", func(t *testing.T) {
		storeNew = func(cfg store.Config) (*store.Store, error) {
			return nil, errors.New("db open failed")
		}
		withArgs(t, "lore", "mcp")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdMCP(cfg) })
		assertFatal(t, stderr, recovered, "db open failed")
	})

	t.Run("serveMCP failure calls fatal", func(t *testing.T) {
		storeNew = store.New
		serveMCP = func(_ *mcpserver.MCPServer, _ ...mcpserver.StdioOption) error {
			return errors.New("stdio failed")
		}
		withArgs(t, "lore", "mcp")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdMCP(cfg) })
		assertFatal(t, stderr, recovered, "stdio failed")
	})
}
