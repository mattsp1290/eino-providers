#!/usr/bin/env bash
set -euo pipefail

max_bytes=1824
max_allocs=36

output="$(go test -run '^$' -bench '^BenchmarkProviderConstruction_Gemini$' -benchmem -benchtime=10x -count=1 .)"
printf '%s\n' "$output"

line="$(printf '%s\n' "$output" | awk '/^BenchmarkProviderConstruction_Gemini-/ { print; exit }')"
if [[ -z "$line" ]]; then
  echo "BenchmarkProviderConstruction_Gemini result not found" >&2
  exit 1
fi

bytes="$(awk '{ for (i = 1; i <= NF; i++) if ($i == "B/op") { print $(i - 1); exit } }' <<<"$line")"
allocs="$(awk '{ for (i = 1; i <= NF; i++) if ($i == "allocs/op") { print $(i - 1); exit } }' <<<"$line")"

if [[ -z "$bytes" || -z "$allocs" ]]; then
  echo "Could not parse B/op and allocs/op from: $line" >&2
  exit 1
fi

if (( bytes > max_bytes )); then
  echo "Gemini provider construction bytes/op regression: got ${bytes}, max ${max_bytes}" >&2
  exit 1
fi

if (( allocs > max_allocs )); then
  echo "Gemini provider construction allocs/op regression: got ${allocs}, max ${max_allocs}" >&2
  exit 1
fi
