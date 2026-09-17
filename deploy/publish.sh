#!/usr/bin/env bash
set -Eeuo pipefail

: "${SOURCE_SHA:?SOURCE_SHA is required}"
: "${SOURCE_BRANCH:?SOURCE_BRANCH is required}"
: "${BACKEND_IMAGE:?BACKEND_IMAGE is required}"
: "${WEB_IMAGE:?WEB_IMAGE is required}"

cd "$(dirname "${BASH_SOURCE[0]}")/.."
runtime_dir="$(mktemp -d)"
trap 'rm -rf "$runtime_dir"' EXIT

source_head="$(git ls-remote --exit-code origin "refs/heads/$SOURCE_BRANCH" | cut -f1)"
if [[ "$source_head" != "$SOURCE_SHA" ]]; then
  echo 'A newer source commit exists; this release will not replace it.'
  exit 0
fi

python3 deploy/render.py --backend-image "$BACKEND_IMAGE" --web-image "$WEB_IMAGE" >"$runtime_dir/apps.yaml"
python3 - "$runtime_dir/release.json" <<'PY'
import json
import os
import sys
from pathlib import Path

release = {key.lower(): os.environ[key] for key in ["SOURCE_SHA", "SOURCE_BRANCH", "BACKEND_IMAGE", "WEB_IMAGE"]}
Path(sys.argv[1]).write_text(json.dumps(release, indent=2) + "\n")
PY

parent_args=()
previous="$(git ls-remote origin refs/heads/gitops | cut -f1)"
if [[ -n "$previous" ]]; then
  git fetch --no-tags origin refs/heads/gitops
  previous="$(git rev-parse FETCH_HEAD)"
  parent_args=(-p "$previous")
fi

export GIT_INDEX_FILE="$runtime_dir/index"
git read-tree --empty
for filename in apps.yaml release.json; do
  blob="$(git hash-object -w "$runtime_dir/$filename")"
  git update-index --add --cacheinfo "100644,$blob,$filename"
done
tree="$(git write-tree)"
if [[ -n "$previous" && "$tree" == "$(git rev-parse "$previous^{tree}")" ]]; then
  echo 'This release is already published.'
  exit 0
fi
export GIT_AUTHOR_NAME='github-actions[bot]'
export GIT_AUTHOR_EMAIL='41898282+github-actions[bot]@users.noreply.github.com'
export GIT_COMMITTER_NAME="$GIT_AUTHOR_NAME"
export GIT_COMMITTER_EMAIL="$GIT_AUTHOR_EMAIL"
commit="$(printf 'deploy: %s\n' "$SOURCE_SHA" | git commit-tree "$tree" "${parent_args[@]}")"
git push origin "$commit:refs/heads/gitops"
echo "Published deployment manifests for $SOURCE_SHA."
