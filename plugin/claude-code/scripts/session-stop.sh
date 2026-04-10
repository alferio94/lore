#!/bin/bash
# Lore — Stop hook for Claude Code (async)
#
# Marks the session as ended via the HTTP API.
# Runs async so it doesn't block Claude's response.

LORE_PORT="${LORE_PORT:-7437}"
LORE_URL="http://127.0.0.1:${LORE_PORT}"

INPUT=$(cat)
SESSION_ID=$(echo "$INPUT" | jq -r '.session_id // empty')

if [ -z "$SESSION_ID" ]; then
  exit 0
fi

curl -sf "${LORE_URL}/sessions/${SESSION_ID}/end" \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{}' \
  > /dev/null 2>&1

exit 0
