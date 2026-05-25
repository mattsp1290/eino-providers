package einoproviders

import (
	"context"
	"fmt"
)

// NewProvider constructs a registered single-shot provider by name.
//
// Backend packages register themselves from init. Consumers must import the
// backend packages they want, usually for side effects. If no backend registered
// name, NewProvider returns an error matching ErrUnknownProvider.
func NewProvider(ctx context.Context, name, model string, opts Options) (Provider, error) {
	ctor, ok := lookupProvider(name)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, name)
	}
	return ctor(ctx, model, opts)
}
