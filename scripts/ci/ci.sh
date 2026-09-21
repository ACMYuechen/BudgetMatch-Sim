#!/usr/bin/env bash

set -Eeuo pipefail

readonly SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

"${SCRIPT_DIR}/go-check.sh"
"${SCRIPT_DIR}/web-check.sh"
"${SCRIPT_DIR}/security-check.sh"
"${SCRIPT_DIR}/container-check.sh"
