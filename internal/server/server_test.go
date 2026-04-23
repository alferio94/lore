package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alferio94/lore/internal/store"
)

type stubListener struct{}

func (stubListener) Accept() (net.Conn, error) { return nil, errors.New("not used") }
func (stubListener) Close() error              { return nil }
func (stubListener) Addr() net.Addr            { return &net.TCPAddr{} }

func TestStartReturnsListenError(t *testing.T) {
	s := New(nil, 7777)
	s.listen = func(network, address string) (net.Listener, error) {
		return nil, errors.New("listen failed")
	}

	err := s.Start()
	if err == nil {
		t.Fatalf("expected start to fail on listen error")
	}
}

func TestStartUsesInjectedServe(t *testing.T) {
	s := New(&store.Store{}, 7777)
	s.listen = func(network, address string) (net.Listener, error) {
		return stubListener{}, nil
	}
	s.serve = func(ln net.Listener, h http.Handler) error {
		if ln == nil || h == nil {
			t.Fatalf("expected listener and handler to be provided")
		}
		return errors.New("serve stopped")
	}

	err := s.Start()
	if err == nil || err.Error() != "serve stopped" {
		t.Fatalf("expected propagated serve error, got %v", err)
	}
}

func TestStartUsesConfiguredHostAddress(t *testing.T) {
	s := NewWithConfig(&store.Store{}, Config{Host: "0.0.0.0", Port: 7777, Version: "v-test"})

	var gotAddr string
	s.listen = func(network, address string) (net.Listener, error) {
		gotAddr = address
		return stubListener{}, nil
	}
	s.serve = func(ln net.Listener, h http.Handler) error {
		return errors.New("serve stopped")
	}

	err := s.Start()
	if err == nil || err.Error() != "serve stopped" {
		t.Fatalf("expected propagated serve error, got %v", err)
	}
	if gotAddr != "0.0.0.0:7777" {
		t.Fatalf("listen address = %q, want %q", gotAddr, "0.0.0.0:7777")
	}
}

func newServerTestStore(t *testing.T) *store.Store {
	t.Helper()
	cfg, err := store.DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig: %v", err)
	}
	cfg.DataDir = t.TempDir()

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

func TestStartUsesDefaultListenWhenListenNil(t *testing.T) {
	s := New(newServerTestStore(t), 0)
	s.listen = nil
	s.serve = func(ln net.Listener, h http.Handler) error {
		if ln == nil || h == nil {
			t.Fatalf("expected non-nil listener and handler")
		}
		_ = ln.Close()
		return errors.New("serve stopped")
	}

	err := s.Start()
	if err == nil || err.Error() != "serve stopped" {
		t.Fatalf("expected propagated serve error, got %v", err)
	}
}

func TestStartUsesDefaultServeWhenServeNil(t *testing.T) {
	s := New(newServerTestStore(t), 7777)
	s.listen = func(network, address string) (net.Listener, error) {
		return stubListener{}, nil
	}
	s.serve = nil

	err := s.Start()
	if err == nil {
		t.Fatalf("expected start to fail when default http.Serve receives failing listener")
	}
}

func TestHandleHealthReturnsOKAndVersionWhenStoreAvailable(t *testing.T) {
	st := newServerTestStore(t)
	srv := NewWithConfig(st, Config{Host: "127.0.0.1", Port: 0, Version: "1.2.3"})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode health body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status = %v, want ok", body["status"])
	}
	if body["service"] != "lore" {
		t.Fatalf("service = %v, want lore", body["service"])
	}
	if body["version"] != "1.2.3" {
		t.Fatalf("version = %v, want 1.2.3", body["version"])
	}
}

func TestHandleHealthReturnsUnavailableWhenStoreIsUnavailable(t *testing.T) {
	st := newServerTestStore(t)
	srv := NewWithConfig(st, Config{Host: "127.0.0.1", Port: 0, Version: "1.2.3"})

	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode health body: %v", err)
	}
	if body["status"] != "error" {
		t.Fatalf("status = %v, want error", body["status"])
	}
	if body["reason"] != "store unavailable" {
		t.Fatalf("reason = %v, want store unavailable", body["reason"])
	}
}

func TestAdditionalServerErrorBranches(t *testing.T) {
	st := newServerTestStore(t)
	srv := New(st, 0)
	h := srv.Handler()

	createReq := httptest.NewRequest(http.MethodPost, "/sessions", strings.NewReader(`{"id":"s-test","project":"engram"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	h.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected session create 201, got %d", createRec.Code)
	}

	getBadIDReq := httptest.NewRequest(http.MethodGet, "/observations/not-a-number", nil)
	getBadIDRec := httptest.NewRecorder()
	h.ServeHTTP(getBadIDRec, getBadIDReq)
	if getBadIDRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid observation id, got %d", getBadIDRec.Code)
	}

	updateNotFoundReq := httptest.NewRequest(http.MethodPatch, "/observations/99999", strings.NewReader(`{"title":"updated"}`))
	updateNotFoundReq.Header.Set("Content-Type", "application/json")
	updateNotFoundRec := httptest.NewRecorder()
	h.ServeHTTP(updateNotFoundRec, updateNotFoundReq)
	if updateNotFoundRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 updating missing observation, got %d", updateNotFoundRec.Code)
	}

	promptBadJSONReq := httptest.NewRequest(http.MethodPost, "/prompts", strings.NewReader("{"))
	promptBadJSONReq.Header.Set("Content-Type", "application/json")
	promptBadJSONRec := httptest.NewRecorder()
	h.ServeHTTP(promptBadJSONRec, promptBadJSONReq)
	if promptBadJSONRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid prompt json, got %d", promptBadJSONRec.Code)
	}

	oversizeBody := bytes.Repeat([]byte("a"), 50<<20+1)
	importTooLargeReq := httptest.NewRequest(http.MethodPost, "/import", bytes.NewReader(oversizeBody))
	importTooLargeReq.Header.Set("Content-Type", "application/json")
	importTooLargeRec := httptest.NewRecorder()
	h.ServeHTTP(importTooLargeRec, importTooLargeReq)
	if importTooLargeRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversize import body, got %d", importTooLargeRec.Code)
	}

	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	validImport, err := json.Marshal(store.ExportData{Version: "0.1.0", ExportedAt: "now"})
	if err != nil {
		t.Fatalf("marshal import payload: %v", err)
	}
	importClosedReq := httptest.NewRequest(http.MethodPost, "/import", bytes.NewReader(validImport))
	importClosedReq.Header.Set("Content-Type", "application/json")
	importClosedRec := httptest.NewRecorder()
	h.ServeHTTP(importClosedRec, importClosedReq)
	if importClosedRec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 importing on closed store, got %d", importClosedRec.Code)
	}
}

// ─── Sync Status Tests ───────────────────────────────────────────────────────

// stubSyncStatusProvider is a fake SyncStatusProvider for tests.
type stubSyncStatusProvider struct {
	status SyncStatus
}

func (s *stubSyncStatusProvider) Status() SyncStatus {
	return s.status
}

func TestSyncStatusNotConfigured(t *testing.T) {
	srv := New(newServerTestStore(t), 0)
	// No sync status provider set — should return enabled: false.
	req := httptest.NewRequest(http.MethodGet, "/sync/status", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["enabled"] != false {
		t.Fatalf("expected enabled=false when no provider, got %v", resp["enabled"])
	}
}

func TestSyncStatusHealthy(t *testing.T) {
	now := time.Now()
	provider := &stubSyncStatusProvider{
		status: SyncStatus{
			Phase:      "healthy",
			LastSyncAt: &now,
		},
	}

	srv := New(newServerTestStore(t), 0)
	srv.SetSyncStatus(provider)

	req := httptest.NewRequest(http.MethodGet, "/sync/status", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["enabled"] != true {
		t.Fatalf("expected enabled=true, got %v", resp["enabled"])
	}
	if resp["phase"] != "healthy" {
		t.Fatalf("expected phase=healthy, got %v", resp["phase"])
	}
}

func TestSyncStatusDegraded(t *testing.T) {
	backoff := time.Now().Add(5 * time.Minute)
	provider := &stubSyncStatusProvider{
		status: SyncStatus{
			Phase:               "push_failed",
			LastError:           "network timeout",
			ConsecutiveFailures: 3,
			BackoffUntil:        &backoff,
		},
	}

	srv := New(newServerTestStore(t), 0)
	srv.SetSyncStatus(provider)

	req := httptest.NewRequest(http.MethodGet, "/sync/status", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["phase"] != "push_failed" {
		t.Fatalf("expected phase=push_failed, got %v", resp["phase"])
	}
	if resp["last_error"] != "network timeout" {
		t.Fatalf("expected last_error=network timeout, got %v", resp["last_error"])
	}
	if resp["consecutive_failures"] != float64(3) {
		t.Fatalf("expected consecutive_failures=3, got %v", resp["consecutive_failures"])
	}
}

// ─── OnWrite Notification Tests ──────────────────────────────────────────────

func TestOnWriteCalledAfterSuccessfulWrites(t *testing.T) {
	st := newServerTestStore(t)
	srv := New(st, 0)
	h := srv.Handler()

	var writeCount atomic.Int32
	srv.SetOnWrite(func() {
		writeCount.Add(1)
	})

	// Create session → should trigger onWrite.
	createReq := httptest.NewRequest(http.MethodPost, "/sessions",
		strings.NewReader(`{"id":"s-test","project":"engram"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	h.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("session create: expected 201, got %d", createRec.Code)
	}
	if writeCount.Load() != 1 {
		t.Fatalf("expected 1 onWrite after session create, got %d", writeCount.Load())
	}

	// End session → should trigger onWrite.
	endReq := httptest.NewRequest(http.MethodPost, "/sessions/s-test/end",
		strings.NewReader(`{"summary":"done"}`))
	endReq.Header.Set("Content-Type", "application/json")
	endRec := httptest.NewRecorder()
	h.ServeHTTP(endRec, endReq)
	if endRec.Code != http.StatusOK {
		t.Fatalf("session end: expected 200, got %d", endRec.Code)
	}
	if writeCount.Load() != 2 {
		t.Fatalf("expected 2 onWrite after session end, got %d", writeCount.Load())
	}

	// Add observation → should trigger onWrite.
	obsBody := `{"session_id":"s-test","type":"test","title":"Test","content":"test content"}`
	obsReq := httptest.NewRequest(http.MethodPost, "/observations",
		strings.NewReader(obsBody))
	obsReq.Header.Set("Content-Type", "application/json")
	obsRec := httptest.NewRecorder()
	h.ServeHTTP(obsRec, obsReq)
	if obsRec.Code != http.StatusCreated {
		t.Fatalf("add observation: expected 201, got %d", obsRec.Code)
	}
	if writeCount.Load() != 3 {
		t.Fatalf("expected 3 onWrite after add observation, got %d", writeCount.Load())
	}

	// Add prompt → should trigger onWrite.
	promptBody := `{"session_id":"s-test","content":"what did we do?"}`
	promptReq := httptest.NewRequest(http.MethodPost, "/prompts",
		strings.NewReader(promptBody))
	promptReq.Header.Set("Content-Type", "application/json")
	promptRec := httptest.NewRecorder()
	h.ServeHTTP(promptRec, promptReq)
	if promptRec.Code != http.StatusCreated {
		t.Fatalf("add prompt: expected 201, got %d", promptRec.Code)
	}
	if writeCount.Load() != 4 {
		t.Fatalf("expected 4 onWrite after add prompt, got %d", writeCount.Load())
	}
}

func TestOnWriteNotCalledOnReadOperations(t *testing.T) {
	st := newServerTestStore(t)
	srv := New(st, 0)
	h := srv.Handler()

	var writeCount atomic.Int32
	srv.SetOnWrite(func() {
		writeCount.Add(1)
	})

	// GET /health → read-only, no onWrite.
	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRec := httptest.NewRecorder()
	h.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("health: expected 200, got %d", healthRec.Code)
	}

	// GET /stats → read-only, no onWrite.
	statsReq := httptest.NewRequest(http.MethodGet, "/stats", nil)
	statsRec := httptest.NewRecorder()
	h.ServeHTTP(statsRec, statsReq)

	// GET /sync/status → read-only, no onWrite.
	syncReq := httptest.NewRequest(http.MethodGet, "/sync/status", nil)
	syncRec := httptest.NewRecorder()
	h.ServeHTTP(syncRec, syncReq)

	if writeCount.Load() != 0 {
		t.Fatalf("expected 0 onWrite calls for read operations, got %d", writeCount.Load())
	}
}

func TestOnWriteNotCalledOnFailedWrites(t *testing.T) {
	st := newServerTestStore(t)
	srv := New(st, 0)
	h := srv.Handler()

	var writeCount atomic.Int32
	srv.SetOnWrite(func() {
		writeCount.Add(1)
	})

	// POST /observations with bad JSON → should NOT trigger onWrite.
	badReq := httptest.NewRequest(http.MethodPost, "/observations",
		strings.NewReader(`{invalid`))
	badReq.Header.Set("Content-Type", "application/json")
	badRec := httptest.NewRecorder()
	h.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad json, got %d", badRec.Code)
	}

	// POST /observations with missing required fields → should NOT trigger onWrite.
	missingReq := httptest.NewRequest(http.MethodPost, "/observations",
		strings.NewReader(`{"session_id":"s-test"}`))
	missingReq.Header.Set("Content-Type", "application/json")
	missingRec := httptest.NewRecorder()
	h.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing fields, got %d", missingRec.Code)
	}

	if writeCount.Load() != 0 {
		t.Fatalf("expected 0 onWrite calls for failed writes, got %d", writeCount.Load())
	}
}

// ─── SetMCPHandler Tests ─────────────────────────────────────────────────────

// TestSetMCPHandlerMountsAtMCP verifies that SetMCPHandler registers
// the provided handler at the /mcp path on the server's mux.
func TestSetMCPHandlerMountsAtMCP(t *testing.T) {
	srv := New(newServerTestStore(t), 0)

	// A simple stub handler that writes 200 + a marker body.
	stub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mcp-ok"))
	})

	srv.SetMCPHandler(stub)

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from /mcp, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "mcp-ok" {
		t.Fatalf("expected body %q, got %q", "mcp-ok", body)
	}
}

// TestSetMCPHandlerPostRoutedCorrectly verifies that POST requests to /mcp
// are also dispatched to the registered MCP handler.
func TestSetMCPHandlerPostRoutedCorrectly(t *testing.T) {
	srv := New(newServerTestStore(t), 0)

	stub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mcp-post-ok"))
	})

	srv.SetMCPHandler(stub)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from POST /mcp, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "mcp-post-ok" {
		t.Fatalf("expected body %q, got %q", "mcp-post-ok", body)
	}
}

// TestRESTRoutesUnaffectedAfterMCPMount verifies that existing REST endpoints
// continue to work correctly after SetMCPHandler is called — no routing conflict.
func TestRESTRoutesUnaffectedAfterMCPMount(t *testing.T) {
	srv := New(newServerTestStore(t), 0)

	// Mount MCP handler first.
	stub := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("mcp-ok"))
	})
	srv.SetMCPHandler(stub)

	h := srv.Handler()

	// GET /health must still return 200 with {"status":"ok"}.
	healthReq := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRec := httptest.NewRecorder()
	h.ServeHTTP(healthRec, healthReq)

	if healthRec.Code != http.StatusOK {
		t.Fatalf("GET /health: expected 200 after MCP mount, got %d", healthRec.Code)
	}
	var healthBody map[string]any
	if err := json.NewDecoder(healthRec.Body).Decode(&healthBody); err != nil {
		t.Fatalf("GET /health: decode response: %v", err)
	}
	if healthBody["status"] != "ok" {
		t.Fatalf("GET /health: expected status=ok, got %v", healthBody["status"])
	}

	// GET /observations/recent must still return 200 (empty list is fine).
	obsReq := httptest.NewRequest(http.MethodGet, "/observations/recent", nil)
	obsRec := httptest.NewRecorder()
	h.ServeHTTP(obsRec, obsReq)

	if obsRec.Code != http.StatusOK {
		t.Fatalf("GET /observations/recent: expected 200 after MCP mount, got %d", obsRec.Code)
	}

	// GET /sync/status must still return 200 (no provider configured → enabled:false).
	syncReq := httptest.NewRequest(http.MethodGet, "/sync/status", nil)
	syncRec := httptest.NewRecorder()
	h.ServeHTTP(syncRec, syncReq)

	if syncRec.Code != http.StatusOK {
		t.Fatalf("GET /sync/status: expected 200 after MCP mount, got %d", syncRec.Code)
	}
	var syncBody map[string]any
	if err := json.NewDecoder(syncRec.Body).Decode(&syncBody); err != nil {
		t.Fatalf("GET /sync/status: decode response: %v", err)
	}
	if syncBody["enabled"] != false {
		t.Fatalf("GET /sync/status: expected enabled=false, got %v", syncBody["enabled"])
	}

	// GET /mcp must still hit the MCP handler (not a REST route).
	mcpReq := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	mcpRec := httptest.NewRecorder()
	h.ServeHTTP(mcpRec, mcpReq)

	if mcpRec.Code != http.StatusOK {
		t.Fatalf("GET /mcp: expected 200, got %d", mcpRec.Code)
	}
	if body := mcpRec.Body.String(); body != "mcp-ok" {
		t.Fatalf("GET /mcp: expected mcp handler body, got %q", body)
	}
}

func TestHandleStatsReturnsInternalServerErrorOnLoaderError(t *testing.T) {
	prev := loadServerStats
	loadServerStats = func(s store.Contract) (*store.Stats, error) {
		return nil, errors.New("stats unavailable")
	}
	t.Cleanup(func() {
		loadServerStats = prev
	})

	s := New(newServerTestStore(t), 0)
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	rec := httptest.NewRecorder()

	s.handleStats(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 stats response, got %d", rec.Code)
	}
}
