#!/usr/bin/env bash

set -Eeuo pipefail

gate_failed=0

if [[ "$CHANGES_RESULT" != "success" ]]; then
  echo "Change detection result: $CHANGES_RESULT" >&2
  gate_failed=1
fi

require_result() {
  local name="$1"
  local required="$2"
  local result="$3"

  if [[ "$required" == "true" && "$result" != "success" ]]; then
    echo "$name was selected but finished with: $result" >&2
    gate_failed=1
  elif [[ "$required" != "true" && "$result" != "skipped" ]]; then
    echo "$name was not selected but finished with: $result" >&2
    gate_failed=1
  fi
}

require_result "Go Check" "$GO_REQUIRED" "$GO_RESULT"
require_result "Web Check" "$WEB_REQUIRED" "$WEB_RESULT"
require_result "Security Check" "$SECURITY_REQUIRED" "$SECURITY_RESULT"
require_result "Container Check" "$CONTAINER_REQUIRED" "$CONTAINER_RESULT"

exit "$gate_failed"
