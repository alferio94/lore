#!/usr/bin/env sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
COMPOSE_FILE="$ROOT_DIR/docker-compose.staging.yml"
ENV_FILE="$ROOT_DIR/.env.example"
DOCKERFILE="$ROOT_DIR/Dockerfile"
STAGING_ENV_FILE="$ROOT_DIR/.env.staging"

fail() {
  printf '%s\n' "ERROR: $1" >&2
  exit 1
}

if [ ! -f "$COMPOSE_FILE" ]; then
  fail "Missing required file: docker-compose.staging.yml"
fi

if [ ! -f "$ENV_FILE" ]; then
  fail "Missing required file: .env.example"
fi

if [ ! -f "$DOCKERFILE" ]; then
  fail "Missing required file: Dockerfile"
fi

if ! command -v docker >/dev/null 2>&1; then
  printf '%s\n' "WARN: docker binary not found; skipping compose validation." >&2
  printf '%s\n' "Runtime/deployment artifact checks passed (compose syntax not validated)."
  exit 0
fi

if ! docker compose version >/dev/null 2>&1; then
  fail "'docker compose' is not available (Docker Compose plugin missing)."
fi

CREATED_STAGING_ENV=0
if [ ! -f "$STAGING_ENV_FILE" ]; then
  cp "$ENV_FILE" "$STAGING_ENV_FILE"
  CREATED_STAGING_ENV=1
fi

cleanup() {
  if [ "$CREATED_STAGING_ENV" -eq 1 ]; then
    rm -f "$STAGING_ENV_FILE"
  fi
}
trap cleanup EXIT

if ! docker compose -f "$COMPOSE_FILE" config --quiet; then
  fail "docker compose config validation failed for docker-compose.staging.yml"
fi

printf '%s\n' "Runtime/deployment artifact checks passed."
