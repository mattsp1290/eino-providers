#!/usr/bin/env bash
set -euo pipefail

# Compile a small external-consumer package against every public native
# constructor. This catches accidental visibility regressions without needing
# credentials or a live provider endpoint.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
module_path="github.com/mattsp1290/eino-providers"
fixture_dir="$(mktemp -d)"
cleanup() { rm -rf "$fixture_dir"; }
trap cleanup EXIT HUP INT TERM

cd "$fixture_dir"
go mod init "agentic-constructor-check" >/dev/null
go mod edit -require "${module_path}@v0.0.0"
go mod edit -replace "${module_path}=${repo_root}"
cat > constructors_test.go <<'EOF'
package consumer

import (
	"context"
	"net/http"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	einoproviders "github.com/mattsp1290/eino-providers"
	"github.com/mattsp1290/eino-providers/claude"
	"github.com/mattsp1290/eino-providers/gemini"
	"github.com/mattsp1290/eino-providers/ollama"
	"github.com/mattsp1290/eino-providers/openai"
	"github.com/mattsp1290/eino-providers/openaicodex"
	"github.com/mattsp1290/eino-providers/opencodego"
)

var (
	_ func(context.Context, claude.AgenticModelConfig) (model.AgenticModel, error) = claude.NewAgenticModel
	_ func(context.Context, gemini.AgenticModelConfig) (model.AgenticModel, error) = gemini.NewAgenticModel
	_ func(context.Context, ollama.AgenticModelConfig) (model.AgenticModel, error) = ollama.NewAgenticModel
	_ func(context.Context, openai.AgenticModelConfig) (model.AgenticModel, error) = openai.NewAgenticModel
	_ func(context.Context, openaicodex.AgenticModelConfig) (model.AgenticModel, error) = openaicodex.NewAgenticModel
	_ func(context.Context, *http.Client, openaicodex.AgenticModelConfig) (model.AgenticModel, error) = openaicodex.NewAgenticModelWithHTTPClient
	_ func(context.Context, opencodego.AgenticModelConfig) (model.AgenticModel, error) = opencodego.NewAgenticModel
	_ func(*schema.AgenticMessage) (*schema.AgenticMessage, einoproviders.AgenticContinuationState, error) = claude.SplitAgenticContinuation
	_ func(*schema.AgenticMessage) (*schema.AgenticMessage, einoproviders.AgenticContinuationState, error) = gemini.SplitAgenticContinuation
	_ func(*schema.AgenticMessage) (*schema.AgenticMessage, einoproviders.AgenticContinuationState, error) = ollama.SplitAgenticContinuation
	_ func(*schema.AgenticMessage) (*schema.AgenticMessage, einoproviders.AgenticContinuationState, error) = openai.SplitAgenticContinuation
	_ func(*schema.AgenticMessage) (*schema.AgenticMessage, einoproviders.AgenticContinuationState, error) = openaicodex.SplitAgenticContinuation
	_ func(*schema.AgenticMessage) (*schema.AgenticMessage, einoproviders.AgenticContinuationState, error) = opencodego.SplitAgenticContinuation
)
EOF
go mod tidy
go test ./...
