#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/docker-compose.postgres.yml"
TMP_DIR="$ROOT_DIR/.tmp/postgres-validate"
DATA_DIR="$TMP_DIR/data"
LOG_FILE="$TMP_DIR/lore.log"
PORT="17437"
BASE_URL="http://127.0.0.1:$PORT"
DATABASE_URL_DEFAULT="postgres://lore:lore@127.0.0.1:5432/lore?sslmode=disable"
DATABASE_URL_VALUE="${DATABASE_URL:-$DATABASE_URL_DEFAULT}"

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

mkdir -p "$DATA_DIR"
rm -f "$LOG_FILE"

cleanup() {
  if [ -n "${APP_PID:-}" ] && kill -0 "$APP_PID" >/dev/null 2>&1; then
    kill "$APP_PID" >/dev/null 2>&1 || true
    wait "$APP_PID" >/dev/null 2>&1 || true
  fi
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
go run ./cmd/lore serve >"$LOG_FILE" 2>&1 &
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

SEARCH_STATUS="$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL/search?q=postgres&project=lore&scope=project&limit=10")"
if [ "$SEARCH_STATUS" != "500" ]; then
  fail "expected unsupported PostgreSQL search slice to return HTTP 500, got $SEARCH_STATUS"
fi

printf '%s\n' "Local PostgreSQL validation passed."
