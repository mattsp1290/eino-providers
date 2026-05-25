# eino-providers

`eino-providers` is a shared Go module for constructing CloudWeGo Eino chat model providers across Claude, OpenAI, OpenAI-Codex, Gemini, and Ollama backends.

```go
package main

import (
	"context"
	"fmt"
	"log"

	einoproviders "github.com/mattsp1290/eino-providers"
	_ "github.com/mattsp1290/eino-providers/claude"
)

func main() {
	ctx := context.Background()

	provider, err := einoproviders.NewProvider(ctx, "claude", "claude-sonnet-4-5", einoproviders.Options{
		APIKey: "your-api-key",
	})
	if err != nil {
		log.Fatal(err)
	}

	text, usage, err := provider.Advise(ctx, "Be concise.", "Summarize Eino.", 512)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s\n%+v\n", text, usage)
}
```

The first consumers are [advisor](https://github.com/mattsp1290/advisor) and [local-symphony](https://github.com/mattsp1290/local-symphony).
