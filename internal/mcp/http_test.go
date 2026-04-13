package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
