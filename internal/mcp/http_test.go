package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	serverpkg "github.com/alferio94/lore/internal/server"
	"github.com/alferio94/lore/internal/store"
	jwtlib "github.com/golang-jwt/jwt/v5"
)

func issueMCPBearerToken(t *testing.T, secret []byte, user store.User) string {
	t.Helper()
	claims := jwtlib.MapClaims{
		"sub":   strconv.FormatInt(user.ID, 10),
		"email": user.Email,
		"name":  user.Name,
		"role":  user.Role,
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(24 * time.Hour).Unix(),
	}
	tok := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	signed, err := tok.SignedString(secret)
	if err != nil {
		t.Fatalf("sign bearer token: %v", err)
	}
	return signed
}

func authorizedMCPPost(t *testing.T, url string, secret []byte, user store.User, body map[string]any) *http.Response {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal MCP request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new MCP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+issueMCPBearerToken(t, secret, user))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

// TestNewHTTPHandlerReturnsNonNil verifies that NewHTTPHandler returns a non-nil
// http.Handler. This is the RED test for task 1.1.
func TestNewHTTPHandlerReturnsNonNil(t *testing.T) {
	s := newMCPTestStore(t)
	handler := NewHTTPHandler(s, "test-project")
	if handler == nil {
		t.Fatal("expected NewHTTPHandler to return a non-nil http.Handler")
	}
}

// TestNewHTTPHandlerIsHTTPHandler verifies the return value satisfies http.Handler.
func TestNewHTTPHandlerIsHTTPHandler(t *testing.T) {
	s := newMCPTestStore(t)
	var _ http.Handler = NewHTTPHandler(s, "test-project")
}

func TestNewHTTPHandlerWithConfigRequiresBearerToken(t *testing.T) {
	s := newMCPTestStore(t)
	handler := NewHTTPHandlerWithConfig(s, HTTPHandlerConfig{
		DefaultProject: "test-project",
		JWTSecret:      []byte("mcp-http-secret-32-bytes-long!!!"),
	})

	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp := postMCPJSONAllowStatus(t, ts.URL, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "auth-test-client",
				"version": "1.0",
			},
		},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 for missing bearer token, got %d", resp.StatusCode)
	}
}

func TestNewHTTPHandlerWithConfigRejectsInvalidBearerToken(t *testing.T) {
	s := newMCPTestStore(t)
	handler := NewHTTPHandlerWithConfig(s, HTTPHandlerConfig{
		DefaultProject: "test-project",
		JWTSecret:      []byte("mcp-http-secret-32-bytes-long!!!"),
	})

	ts := httptest.NewServer(handler)
	defer ts.Close()

	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer definitely-not-a-jwt")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST tools/list: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 for invalid bearer token, got %d", resp.StatusCode)
	}
}

func TestNewHTTPHandlerWithConfigRejectsDisabledActorFromStore(t *testing.T) {
	s := newMCPTestStore(t)
	secret := []byte("mcp-http-secret-32-bytes-long!!!")
	handler := NewHTTPHandlerWithConfig(s, HTTPHandlerConfig{DefaultProject: "test-project", JWTSecret: secret})

	user, err := s.CreatePendingUser("disabled@example.com", "Disabled User", "hash-disabled")
	if err != nil {
		t.Fatalf("CreatePendingUser: %v", err)
	}
	user, err = s.UpdateUserStatusRole(user.ID, store.UserStatusActive, store.UserRoleDeveloper)
	if err != nil {
		t.Fatalf("UpdateUserStatusRole(active): %v", err)
	}
	user, err = s.UpdateUserStatusRole(user.ID, store.UserStatusDisabled, store.UserRoleDeveloper)
	if err != nil {
		t.Fatalf("UpdateUserStatusRole(disabled): %v", err)
	}

	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp := authorizedMCPPost(t, ts.URL, secret, *user, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected HTTP 403 for disabled actor, got %d", resp.StatusCode)
	}
}

func TestNewHTTPHandlerWithConfigRejectsStaleTokenActor(t *testing.T) {
	s := newMCPTestStore(t)
	secret := []byte("mcp-http-secret-32-bytes-long!!!")
	handler := NewHTTPHandlerWithConfig(s, HTTPHandlerConfig{DefaultProject: "test-project", JWTSecret: secret})

	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp := authorizedMCPPost(t, ts.URL, secret, store.User{
		ID:     99999,
		Email:  "ghost@example.com",
		Name:   "Ghost",
		Role:   store.UserRoleDeveloper,
		Status: store.UserStatusActive,
	}, map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected HTTP 403 for stale-token actor, got %d", resp.StatusCode)
	}
}

func TestNewHTTPHandlerWithConfigDeveloperGetsAgentToolsOnly(t *testing.T) {
	s := newMCPTestStore(t)
	secret := []byte("mcp-http-secret-32-bytes-long!!!")
	handler := NewHTTPHandlerWithConfig(s, HTTPHandlerConfig{DefaultProject: "test-project", JWTSecret: secret})

	user, err := s.CreatePendingUser("developer@example.com", "Developer", "hash-developer")
	if err != nil {
		t.Fatalf("CreatePendingUser: %v", err)
	}
	user, err = s.UpdateUserStatusRole(user.ID, store.UserStatusActive, store.UserRoleDeveloper)
	if err != nil {
		t.Fatalf("UpdateUserStatusRole: %v", err)
	}

	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp := authorizedMCPPost(t, ts.URL, secret, *user, map[string]any{
		"jsonrpc": "2.0",
		"id":      5,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	body := decodeMCPBodyAllowStatus(t, resp, http.StatusOK)

	toolNames := mcpToolNamesFromBody(t, body)
	for _, tool := range []string{"lore_save", "lore_search", "lore_update", "lore_list_skills", "lore_get_skill"} {
		if !toolNames[tool] {
			t.Fatalf("expected developer tools/list to include %q, got %v", tool, toolNames)
		}
	}
	for _, tool := range []string{"lore_delete", "lore_merge_projects", "lore_stats", "lore_timeline"} {
		if toolNames[tool] {
			t.Fatalf("expected developer tools/list to exclude %q, got %v", tool, toolNames)
		}
	}
}

func TestNewHTTPHandlerWithConfigAdminGetsAdminTools(t *testing.T) {
	s := newMCPTestStore(t)
	secret := []byte("mcp-http-secret-32-bytes-long!!!")
	handler := NewHTTPHandlerWithConfig(s, HTTPHandlerConfig{DefaultProject: "test-project", JWTSecret: secret})

	user, err := s.BootstrapAdmin("admin@example.com", "Admin", "hash-admin")
	if err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}

	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp := authorizedMCPPost(t, ts.URL, secret, *user, map[string]any{
		"jsonrpc": "2.0",
		"id":      6,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	body := decodeMCPBodyAllowStatus(t, resp, http.StatusOK)

	toolNames := mcpToolNamesFromBody(t, body)
	for _, tool := range []string{"lore_save", "lore_delete", "lore_merge_projects", "lore_stats", "lore_timeline"} {
		if !toolNames[tool] {
			t.Fatalf("expected admin tools/list to include %q, got %v", tool, toolNames)
		}
	}
}

func TestNewHTTPHandlerWithConfigUsesStoreRoleInsteadOfTokenClaims(t *testing.T) {
	s := newMCPTestStore(t)
	secret := []byte("mcp-http-secret-32-bytes-long!!!")
	handler := NewHTTPHandlerWithConfig(s, HTTPHandlerConfig{DefaultProject: "test-project", JWTSecret: secret})

	user, err := s.CreatePendingUser("role-override@example.com", "Role Override", "hash-role-override")
	if err != nil {
		t.Fatalf("CreatePendingUser: %v", err)
	}
	user, err = s.UpdateUserStatusRole(user.ID, store.UserStatusActive, store.UserRoleDeveloper)
	if err != nil {
		t.Fatalf("UpdateUserStatusRole: %v", err)
	}

	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp := authorizedMCPPost(t, ts.URL, secret, store.User{
		ID:     user.ID,
		Email:  user.Email,
		Name:   user.Name,
		Role:   store.UserRoleAdmin,
		Status: user.Status,
	}, map[string]any{
		"jsonrpc": "2.0",
		"id":      7,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	body := decodeMCPBodyAllowStatus(t, resp, http.StatusOK)

	toolNames := mcpToolNamesFromBody(t, body)
	if toolNames["lore_delete"] || toolNames["lore_merge_projects"] {
		t.Fatalf("expected store role to override admin claims, got admin tools %v", toolNames)
	}
	if !toolNames["lore_save"] || !toolNames["lore_get_skill"] {
		t.Fatalf("expected developer-level tools from current store role, got %v", toolNames)
	}
}

// TestMCPHTTPInitializeRoundTrip verifies that a valid MCP initialize request
// returns HTTP 200 and a response body containing "serverInfo" with name "lore".
func TestMCPHTTPInitializeRoundTrip(t *testing.T) {
	s := newMCPTestStore(t)
	handler := NewHTTPHandler(s, "test-project")

	ts := httptest.NewServer(handler)
	defer ts.Close()

	initReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0",
			},
		},
	}

	body, err := json.Marshal(initReq)
	if err != nil {
		t.Fatalf("marshal initialize request: %v", err)
	}

	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST initialize: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	resultBody, ok := result["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'result' field in response, got: %v", result)
	}

	serverInfo, ok := resultBody["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'serverInfo' in result, got: %v", resultBody)
	}

	name, _ := serverInfo["name"].(string)
	if name != "lore" {
		t.Fatalf("expected serverInfo.name = %q, got %q", "lore", name)
	}
}

// TestMCPHTTPToolsListReturnsAllLoreTools verifies that tools/list via HTTP returns
// all 15 lore_* tools registered in the MCP server.
func TestMCPHTTPToolsListReturnsAllLoreTools(t *testing.T) {
	s := newMCPTestStore(t)
	handler := NewHTTPHandler(s, "test-project")

	ts := httptest.NewServer(handler)
	defer ts.Close()

	toolsReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	}

	body, err := json.Marshal(toolsReq)
	if err != nil {
		t.Fatalf("marshal tools/list request: %v", err)
	}

	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST tools/list: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	resultBody, ok := result["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'result' field in response, got: %v", result)
	}

	tools, ok := resultBody["tools"].([]any)
	if !ok {
		t.Fatalf("expected 'tools' array in result, got: %v", resultBody)
	}

	// Build set of returned tool names
	toolNames := make(map[string]bool, len(tools))
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		if !ok {
			continue
		}
		name, _ := toolMap["name"].(string)
		toolNames[name] = true
	}

	// All 15 lore_* tools must be present
	expectedTools := []string{
		"lore_save",
		"lore_search",
		"lore_context",
		"lore_session_summary",
		"lore_session_start",
		"lore_session_end",
		"lore_get_observation",
		"lore_suggest_topic_key",
		"lore_capture_passive",
		"lore_save_prompt",
		"lore_update",
		"lore_delete",
		"lore_stats",
		"lore_timeline",
		"lore_merge_projects",
	}

	for _, tool := range expectedTools {
		if !toolNames[tool] {
			t.Errorf("expected tool %q in tools/list response, but it was missing", tool)
		}
	}

	if t.Failed() {
		t.Logf("tools returned: %v", toolNames)
	}
}

// TestMCPHTTPInvalidJSONReturnsJSONRPCError verifies that sending malformed JSON
// to POST /mcp returns an MCP-compliant JSON-RPC error envelope rather than a raw
// HTTP error. The StreamableHTTPServer handles parse errors internally and always
// wraps them in a JSON-RPC error body with "jsonrpc" and "error" fields.
//
// Regression guard: if the underlying mcp-go library changes this behavior and
// starts returning raw HTTP errors, this test will catch the regression.
func TestMCPHTTPInvalidJSONReturnsJSONRPCError(t *testing.T) {
	s := newMCPTestStore(t)
	handler := NewHTTPHandler(s, "test-project")

	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Send malformed JSON body
	malformed := bytes.NewReader([]byte(`{this is not valid json`))
	resp, err := http.Post(ts.URL, "application/json", malformed)
	if err != nil {
		t.Fatalf("POST malformed JSON: %v", err)
	}
	defer resp.Body.Close()

	// The StreamableHTTPServer returns HTTP 400 with a JSON-RPC parse error
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response body: %v — expected JSON-RPC error envelope", err)
	}

	// Must have "jsonrpc" field at the top level
	if _, ok := result["jsonrpc"]; !ok {
		t.Fatalf("expected 'jsonrpc' field in error response, got: %v", result)
	}

	// Must have "error" field at the top level
	if _, ok := result["error"]; !ok {
		t.Fatalf("expected 'error' field in error response, got: %v", result)
	}

	// The error object must have a "code" and "message"
	errObj, ok := result["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'error' to be an object, got: %T", result["error"])
	}
	if _, ok := errObj["code"]; !ok {
		t.Fatalf("expected 'code' in error object, got: %v", errObj)
	}
	if _, ok := errObj["message"]; !ok {
		t.Fatalf("expected 'message' in error object, got: %v", errObj)
	}
}

// TestMCPHTTPToolsCallEndToEnd verifies a full tools/call round-trip over HTTP.
// It uses lore_stats — a read-only tool with no required parameters — to keep
// the test self-contained without needing to seed any observations.
func TestMCPHTTPToolsCallEndToEnd(t *testing.T) {
	s := newMCPTestStore(t)
	handler := NewHTTPHandler(s, "test-project")

	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Call tools/call for lore_stats — no parameters needed
	toolsCallReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "lore_stats",
			"arguments": map[string]any{},
		},
	}

	body, err := json.Marshal(toolsCallReq)
	if err != nil {
		t.Fatalf("marshal tools/call request: %v", err)
	}

	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST tools/call: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode tools/call response: %v", err)
	}

	// Must have a "result" field (not an error)
	resultBody, ok := result["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'result' field in response, got: %v", result)
	}

	// The result must contain "content" (MCP CallToolResult schema)
	content, ok := resultBody["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected non-empty 'content' array in result, got: %v", resultBody)
	}

	// The first content item must be a text block containing stats output
	firstItem, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("expected content[0] to be an object, got: %T", content[0])
	}
	if firstItem["type"] != "text" {
		t.Fatalf("expected content[0].type = 'text', got: %v", firstItem["type"])
	}
	text, _ := firstItem["text"].(string)
	if text == "" {
		t.Fatalf("expected non-empty text in tools/call result")
	}

	// lore_stats output always contains "Sessions:" and "Observations:"
	if !strings.Contains(text, "Sessions:") || !strings.Contains(text, "Observations:") {
		t.Fatalf("expected lore_stats output with Sessions/Observations, got: %q", text)
	}
}

// TestMCPHTTPCrossTransportDataVisibility verifies that data saved via lore_save
// over MCP HTTP is immediately visible to lore_search over the same transport.
// This proves that the shared *store.Store instance correctly persists state
// between tool calls within the same HTTP handler.
func TestMCPHTTPCrossTransportDataVisibility(t *testing.T) {
	s := newMCPTestStore(t)
	handler := NewHTTPHandler(s, "test-project")

	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Step 1: save an observation via lore_save
	const uniqueTitle = "cross-transport-visibility-marker-xT7k"
	savReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      10,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "lore_save",
			"arguments": map[string]any{
				"title":   uniqueTitle,
				"content": "Written via MCP HTTP to verify shared store visibility",
				"type":    "discovery",
				"project": "test-project",
			},
		},
	}

	saveBody, err := json.Marshal(savReq)
	if err != nil {
		t.Fatalf("marshal lore_save request: %v", err)
	}

	saveResp, err := http.Post(ts.URL, "application/json", bytes.NewReader(saveBody))
	if err != nil {
		t.Fatalf("POST lore_save: %v", err)
	}
	defer saveResp.Body.Close()

	if saveResp.StatusCode != http.StatusOK {
		t.Fatalf("lore_save: expected HTTP 200, got %d", saveResp.StatusCode)
	}

	var saveResult map[string]any
	if err := json.NewDecoder(saveResp.Body).Decode(&saveResult); err != nil {
		t.Fatalf("decode lore_save response: %v", err)
	}
	if _, hasErr := saveResult["error"]; hasErr {
		t.Fatalf("lore_save returned error: %v", saveResult)
	}

	// Step 2: search for that observation via lore_search over the same handler
	searchReq := map[string]any{
		"jsonrpc": "2.0",
		"id":      11,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "lore_search",
			"arguments": map[string]any{
				"query":   uniqueTitle,
				"project": "test-project",
				"limit":   5,
			},
		},
	}

	searchBody, err := json.Marshal(searchReq)
	if err != nil {
		t.Fatalf("marshal lore_search request: %v", err)
	}

	searchResp, err := http.Post(ts.URL, "application/json", bytes.NewReader(searchBody))
	if err != nil {
		t.Fatalf("POST lore_search: %v", err)
	}
	defer searchResp.Body.Close()

	if searchResp.StatusCode != http.StatusOK {
		t.Fatalf("lore_search: expected HTTP 200, got %d", searchResp.StatusCode)
	}

	var searchResult map[string]any
	if err := json.NewDecoder(searchResp.Body).Decode(&searchResult); err != nil {
		t.Fatalf("decode lore_search response: %v", err)
	}

	resultBody, ok := searchResult["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'result' field in lore_search response, got: %v", searchResult)
	}

	content, ok := resultBody["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected non-empty content in lore_search result, got: %v", resultBody)
	}

	firstItem, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("expected content[0] to be an object, got: %T", content[0])
	}

	text, _ := firstItem["text"].(string)
	if !strings.Contains(text, uniqueTitle) {
		t.Fatalf("lore_search did not find the saved observation %q in result: %q", uniqueTitle, text)
	}
}

func TestMCPHTTPPostgresRoundTripAtServerMCPPath(t *testing.T) {
	pg := newPostgresMCPTestStore(t)
	srv := serverpkg.NewWithConfig(pg, serverpkg.Config{Host: "127.0.0.1", Port: 0, Version: "pg-preview"})
	srv.SetMCPHandler(NewHTTPHandler(pg, "preview-runtime"))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	initializeResp := postMCPJSON(t, ts.URL+"/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      100,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "postgres-smoke",
				"version": "1.0",
			},
		},
	})
	initializeBody := decodeMCPBody(t, initializeResp)
	initializeResult, ok := initializeBody["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize result missing: %v", initializeBody)
	}
	serverInfo, ok := initializeResult["serverInfo"].(map[string]any)
	if !ok || serverInfo["name"] != "lore" {
		t.Fatalf("initialize serverInfo = %v, want lore", initializeResult["serverInfo"])
	}

	const (
		project = "preview-runtime"
		title   = "postgres http smoke marker"
		content = "Written through /mcp against PostgreSQL preview storage"
	)

	saveResp := postMCPJSON(t, ts.URL+"/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      101,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "lore_save",
			"arguments": map[string]any{
				"title":   title,
				"content": content,
				"type":    "discovery",
				"project": project,
			},
		},
	})
	saveText := firstMCPText(t, decodeMCPBody(t, saveResp))
	if !strings.Contains(saveText, fmt.Sprintf("Memory saved: %q", title)) {
		t.Fatalf("save response = %q, want memory saved confirmation", saveText)
	}

	searchResp := postMCPJSON(t, ts.URL+"/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      102,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "lore_search",
			"arguments": map[string]any{
				"query":   title,
				"project": project,
				"limit":   5,
			},
		},
	})
	searchText := firstMCPText(t, decodeMCPBody(t, searchResp))
	if !strings.Contains(searchText, title) || !strings.Contains(searchText, "fallback_used=false") {
		t.Fatalf("search response = %q, want saved title and exact-path fallback metadata", searchText)
	}

	match := regexp.MustCompile(`(?m)\[1\] #(\d+)`).FindStringSubmatch(searchText)
	if len(match) != 2 {
		t.Fatalf("search response = %q, want first observation id", searchText)
	}
	observationID, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatalf("parse observation id %q: %v", match[1], err)
	}

	getResp := postMCPJSON(t, ts.URL+"/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      103,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "lore_get_observation",
			"arguments": map[string]any{
				"id": observationID,
			},
		},
	})
	getText := firstMCPText(t, decodeMCPBody(t, getResp))
	if !strings.Contains(getText, title) || !strings.Contains(getText, content) || !strings.Contains(getText, "Project: "+project) {
		t.Fatalf("get response = %q, want stored title/content/project", getText)
	}
}

func TestAuthenticatedMCPHTTPMountRejectsMissingBearerAtServerPath(t *testing.T) {
	s := newMCPTestStore(t)
	secret := []byte("mcp-http-secret-32-bytes-long!!!")
	srv := serverpkg.NewWithConfig(s, serverpkg.Config{Host: "127.0.0.1", Port: 0, Version: "auth-preview"})
	srv.SetMCPHandler(NewHTTPHandlerWithConfig(s, HTTPHandlerConfig{DefaultProject: "preview-runtime", JWTSecret: secret}))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := postMCPJSONAllowStatus(t, ts.URL+"/mcp", map[string]any{
		"jsonrpc": "2.0",
		"id":      104,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "missing-bearer",
				"version": "1.0",
			},
		},
	})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 for mounted /mcp without bearer token, got %d", resp.StatusCode)
	}
}

func postMCPJSON(t *testing.T, url string, body map[string]any) *http.Response {
	t.Helper()
	resp := postMCPJSONAllowStatus(t, url, body)
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		var raw bytes.Buffer
		_, _ = raw.ReadFrom(resp.Body)
		t.Fatalf("POST %s: expected HTTP 200, got %d body=%q", url, resp.StatusCode, raw.String())
	}
	return resp
}

func postMCPJSONAllowStatus(t *testing.T, url string, body map[string]any) *http.Response {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal MCP request: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}

func decodeMCPBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	return decodeMCPBodyAllowStatus(t, resp, http.StatusOK)
}

func decodeMCPBodyAllowStatus(t *testing.T, resp *http.Response, status int) map[string]any {
	t.Helper()
	if resp.StatusCode != status {
		defer resp.Body.Close()
		var raw bytes.Buffer
		_, _ = raw.ReadFrom(resp.Body)
		t.Fatalf("expected HTTP %d, got %d body=%q", status, resp.StatusCode, raw.String())
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode MCP response: %v", err)
	}
	if errBody, hasErr := body["error"]; hasErr {
		t.Fatalf("unexpected MCP error: %v", errBody)
	}
	return body
}

func mcpToolNamesFromBody(t *testing.T, body map[string]any) map[string]bool {
	t.Helper()
	resultBody, ok := body["result"].(map[string]any)
	if !ok {
		t.Fatalf("result missing from MCP body: %v", body)
	}
	tools, ok := resultBody["tools"].([]any)
	if !ok {
		t.Fatalf("tools missing from MCP body: %v", resultBody)
	}
	toolNames := make(map[string]bool, len(tools))
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		if !ok {
			continue
		}
		name, _ := toolMap["name"].(string)
		toolNames[name] = true
	}
	return toolNames
}

func firstMCPText(t *testing.T, body map[string]any) string {
	t.Helper()
	resultBody, ok := body["result"].(map[string]any)
	if !ok {
		t.Fatalf("result missing from MCP body: %v", body)
	}
	content, ok := resultBody["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("content missing from MCP body: %v", resultBody)
	}
	firstItem, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] has unexpected type %T", content[0])
	}
	text, _ := firstItem["text"].(string)
	if text == "" {
		t.Fatalf("expected non-empty text content: %v", firstItem)
	}
	return text
}
