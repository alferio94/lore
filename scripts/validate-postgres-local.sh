#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/docker-compose.postgres.yml"
TMP_DIR="$ROOT_DIR/.tmp/postgres-validate"
DATA_DIR="$TMP_DIR/data"
LOG_FILE="$TMP_DIR/lore.log"
APP_BIN="$TMP_DIR/lore-validate"
PORT="17437"
BASE_URL="http://127.0.0.1:$PORT"
DATABASE_URL_DEFAULT="postgres://lore:lore@127.0.0.1:5432/lore?sslmode=disable"
DATABASE_URL_VALUE="${DATABASE_URL:-$DATABASE_URL_DEFAULT}"
SKILL_NAME="pg-local-hosted-skill-$(date +%s)"
SKILL_DISPLAY_NAME="PG Local Hosted Skill"
SKILL_TRIGGERS="When validating hosted postgres MCP skill reads"
SKILL_CONTENT="# Postgres hosted validation skill"
SKILL_COMPACT_RULES="Keep answers terse."
SEED_FILE="$TMP_DIR/seed_skill.go"
SMOKE_FILE="$TMP_DIR/mcp_skill_smoke.go"

export SKILL_NAME SKILL_DISPLAY_NAME SKILL_TRIGGERS SKILL_CONTENT SKILL_COMPACT_RULES

fail() {
  printf '%s\n' "ERROR: $1" >&2
  exit 1
}

if [ ! -f "$COMPOSE_FILE" ]; then
  fail "Missing required file: docker-compose.postgres.yml"
fi

if ! command -v docker >/dev/null 2>&1; then
  fail "docker binary not found"
fi

if ! docker compose version >/dev/null 2>&1; then
  fail "'docker compose' is not available (Docker Compose plugin missing)"
fi

if ! command -v go >/dev/null 2>&1; then
  fail "go binary not found"
fi

if ! command -v curl >/dev/null 2>&1; then
  fail "curl binary not found"
fi

if ! command -v python3 >/dev/null 2>&1; then
  fail "python3 binary not found"
fi

rm -rf "$TMP_DIR"
mkdir -p "$DATA_DIR"
rm -f "$LOG_FILE"

cleanup() {
  if [ -n "${APP_PID:-}" ] && kill -0 "$APP_PID" >/dev/null 2>&1; then
    kill "$APP_PID" >/dev/null 2>&1 || true
    wait "$APP_PID" >/dev/null 2>&1 || true
  fi
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

docker compose -f "$COMPOSE_FILE" up -d postgres

attempt=0
while [ "$attempt" -lt 30 ]; do
  attempt=$((attempt + 1))
  if docker compose -f "$COMPOSE_FILE" exec -T postgres pg_isready -U lore -d lore >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! docker compose -f "$COMPOSE_FILE" exec -T postgres pg_isready -U lore -d lore >/dev/null 2>&1; then
  fail "postgres did not become ready"
fi

LORE_DATA_DIR="$DATA_DIR" \
LORE_PORT="$PORT" \
DATABASE_URL="$DATABASE_URL_VALUE" \
go build -o "$APP_BIN" ./cmd/lore

LORE_DATA_DIR="$DATA_DIR" \
LORE_PORT="$PORT" \
DATABASE_URL="$DATABASE_URL_VALUE" \
"$APP_BIN" serve >"$LOG_FILE" 2>&1 &
APP_PID=$!

attempt=0
while [ "$attempt" -lt 30 ]; do
  attempt=$((attempt + 1))
  if curl -fsS "$BASE_URL/health" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! curl -fsS "$BASE_URL/health" >/dev/null 2>&1; then
  if [ -f "$LOG_FILE" ]; then
    printf '%s\n' "Lore server logs:" >&2
    cat "$LOG_FILE" >&2
  fi
  fail "lore serve did not become healthy"
fi

curl -fsS -X POST "$BASE_URL/sessions" \
  -H 'Content-Type: application/json' \
  -d '{"id":"pg-local-session","project":"lore","directory":"/tmp/lore"}' >/dev/null

CREATE_RESPONSE="$(curl -fsS -X POST "$BASE_URL/observations" \
  -H 'Content-Type: application/json' \
  -d '{"session_id":"pg-local-session","type":"architecture","title":"Postgres local validation","content":"Validate postgres-backed health and observation flow locally","project":"lore","scope":"project","topic_key":"architecture/postgres-local-validation"}')"

OBS_ID="$(printf '%s' "$CREATE_RESPONSE" | python3 -c 'import json,sys; print(int(json.load(sys.stdin)["id"]))')"

curl -fsS "$BASE_URL/observations/$OBS_ID" >/dev/null

curl -fsS -X PATCH "$BASE_URL/observations/$OBS_ID" \
  -H 'Content-Type: application/json' \
  -d '{"content":"Validate postgres-backed health, update, and delete flow locally"}' >/dev/null

curl -fsS "$BASE_URL/timeline?observation_id=$OBS_ID&before=1&after=1" >/dev/null

curl -fsS -X DELETE "$BASE_URL/observations/$OBS_ID" >/dev/null

curl -fsS -X POST "$BASE_URL/sessions/pg-local-session/end" \
  -H 'Content-Type: application/json' \
  -d '{"summary":"local postgres validation complete"}' >/dev/null

cat >"$SEED_FILE" <<'EOF'
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/alferio94/lore/internal/store"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		panic("DATABASE_URL is required")
	}

	opened, err := store.Open(store.Config{
		Backend:              store.BackendPostgreSQL,
		DatabaseURL:          databaseURL,
		MaxObservationLength: 50000,
		MaxContextResults:    20,
		MaxSearchResults:     20,
		DedupeWindow:         15 * time.Minute,
	})
	if err != nil {
		panic(fmt.Sprintf("open postgres store: %v", err))
	}
	defer opened.Close()

	if _, err := opened.CreateSkill(store.CreateSkillParams{
		Name:         os.Getenv("SKILL_NAME"),
		DisplayName:  os.Getenv("SKILL_DISPLAY_NAME"),
		Triggers:     os.Getenv("SKILL_TRIGGERS"),
		Content:      os.Getenv("SKILL_CONTENT"),
		CompactRules: os.Getenv("SKILL_COMPACT_RULES"),
		ChangedBy:    "validate-postgres-local",
	}); err != nil {
		panic(fmt.Sprintf("create skill: %v", err))
	}
}
EOF

DATABASE_URL="$DATABASE_URL_VALUE" \
SKILL_NAME="$SKILL_NAME" \
SKILL_DISPLAY_NAME="$SKILL_DISPLAY_NAME" \
SKILL_TRIGGERS="$SKILL_TRIGGERS" \
SKILL_CONTENT="$SKILL_CONTENT" \
SKILL_COMPACT_RULES="$SKILL_COMPACT_RULES" \
go run "$SEED_FILE" >/dev/null

cat >"$SMOKE_FILE" <<'EOF'
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func main() {
	baseURL := strings.TrimRight(os.Getenv("BASE_URL"), "/")
	if baseURL == "" {
		panic("BASE_URL is required")
	}
	mcpURL := baseURL + "/mcp"
	skillName := os.Getenv("SKILL_NAME")
	compactRules := os.Getenv("SKILL_COMPACT_RULES")
	skillContent := os.Getenv("SKILL_CONTENT")

	_ = post(mcpURL, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "postgres-local-validator",
				"version": "1.0",
			},
		},
	})

	listBody := post(mcpURL, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "lore_list_skills",
			"arguments": map[string]any{
				"query": skillName,
			},
		},
	})
	listText := firstText(listBody)
	if !strings.Contains(listText, skillName) || strings.Contains(listText, "compact_rules") {
		panic(fmt.Sprintf("unexpected lore_list_skills response: %s", listText))
	}

	getBody := post(mcpURL, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "lore_get_skill",
			"arguments": map[string]any{
				"name": skillName,
			},
		},
	})
	getText := firstText(getBody)
	if !strings.Contains(getText, skillName) || !strings.Contains(getText, compactRules) || !strings.Contains(getText, skillContent) {
		panic(fmt.Sprintf("unexpected lore_get_skill response: %s", getText))
	}

	resourceBody := post(mcpURL, map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "resources/read",
		"params": map[string]any{
			"uri": "skills://" + skillName,
		},
	})
	result, ok := resourceBody["result"].(map[string]any)
	if !ok {
		panic(fmt.Sprintf("resource result missing: %v", resourceBody))
	}
	contents, ok := result["contents"].([]any)
	if !ok || len(contents) == 0 {
		panic(fmt.Sprintf("resource contents missing: %v", result["contents"]))
	}
	resource, ok := contents[0].(map[string]any)
	if !ok {
		panic(fmt.Sprintf("unexpected resource payload: %T", contents[0]))
	}
	if resource["mimeType"] != "text/markdown" || resource["uri"] != "skills://"+skillName || !strings.Contains(fmt.Sprint(resource["text"]), skillContent) {
		panic(fmt.Sprintf("unexpected resource response: %+v", resource))
	}
}

func post(url string, payload map[string]any) map[string]any {
	body, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("marshal payload: %v", err))
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		panic(fmt.Sprintf("POST %s: %v", url, err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		panic(fmt.Sprintf("POST %s: unexpected HTTP %d body=%s", url, resp.StatusCode, string(raw)))
	}
	var decoded map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		panic(fmt.Sprintf("decode response: %v", err))
	}
	return decoded
}

func firstText(body map[string]any) string {
	result, ok := body["result"].(map[string]any)
	if !ok {
		panic(fmt.Sprintf("tool result missing: %v", body))
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		panic(fmt.Sprintf("tool content missing: %v", result["content"]))
	}
	item, ok := content[0].(map[string]any)
	if !ok {
		panic(fmt.Sprintf("unexpected content payload: %T", content[0]))
	}
	text, _ := item["text"].(string)
	if text == "" {
		panic(fmt.Sprintf("missing text payload: %v", item))
	}
	return text
}
EOF

DATABASE_URL="$DATABASE_URL_VALUE" \
BASE_URL="$BASE_URL" \
SKILL_NAME="$SKILL_NAME" \
SKILL_COMPACT_RULES="$SKILL_COMPACT_RULES" \
SKILL_CONTENT="$SKILL_CONTENT" \
go run "$SMOKE_FILE" >/dev/null

printf '%s\n' "Local PostgreSQL validation passed."
