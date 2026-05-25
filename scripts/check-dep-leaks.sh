#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
module_path="github.com/mattsp1290/eino-providers"
for backend in claude openai gemini ollama; do
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  (
    cd "$tmp"
    go mod init depcheck >/dev/null
    go mod edit -require "${module_path}@v0.0.0"
    go mod edit -replace "${module_path}=${repo_root}"
    cat > main.go <<EOF
package main

import _ "${module_path}/${backend}"

func main() {}
EOF
    go mod tidy >/dev/null
    if go list -deps . | grep -Fqx "github.com/mattsp1290/codex-auth-go"; then
      echo "${backend} unexpectedly depends on github.com/mattsp1290/codex-auth-go" >&2
      exit 1
    fi
  )
  rm -rf "$tmp"
  trap - EXIT
done
