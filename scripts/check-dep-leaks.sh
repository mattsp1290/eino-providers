#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
module_path="github.com/mattsp1290/eino-providers"
workdir="$(mktemp -d)"
cleanup() {
  rm -rf "$workdir"
}
trap cleanup EXIT HUP INT TERM

check_import() {
  local label="$1"
  local import_path="$2"
  shift 2
  local fixture="${workdir}/${label}"
  mkdir -p "$fixture"
  (
    cd "$fixture"
    go mod init "depcheck/${label}" >/dev/null
    go mod edit -require "${module_path}@v0.0.0"
    go mod edit -replace "${module_path}=${repo_root}"
    cat > main.go <<EOF
package main

import _ "${import_path}"

func main() {}
EOF
    go mod tidy >/dev/null
    go build .
    deps="$(go list -deps .)"
    for forbidden in "$@"; do
      if grep -Fqx "$forbidden" <<<"$deps"; then
        echo "${label} unexpectedly depends on ${forbidden}" >&2
        exit 1
      fi
    done
  )
}

codex_auth="github.com/mattsp1290/codex-auth-go"
opencode_auth="github.com/mattsp1290/opencode-auth-go"

check_import root "$module_path" "$codex_auth" "$opencode_auth"
for backend in claude openai gemini ollama; do
  check_import "$backend" "${module_path}/${backend}" "$codex_auth" "$opencode_auth"
done
check_import opencodego "${module_path}/opencodego" "$codex_auth"
