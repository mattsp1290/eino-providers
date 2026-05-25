package einoproviders

import (
	"context"
	"fmt"
	"sync"
)

// ProviderCtor constructs a single-shot Provider for a registered backend.
type ProviderCtor func(ctx context.Context, model string, opts Options) (Provider, error)

var providerRegistry = struct {
	sync.RWMutex
	ctors map[string]ProviderCtor
}{
	ctors: make(map[string]ProviderCtor),
}

// RegisterProvider registers a provider constructor by name.
//
// Backend packages should call RegisterProvider from init. Duplicate names are
// programming errors and panic, matching database/sql driver registration.
func RegisterProvider(name string, ctor ProviderCtor) {
	if name == "" {
		panic("einoproviders: RegisterProvider with empty name")
	}
	if ctor == nil {
		panic("einoproviders: RegisterProvider with nil constructor")
	}

	providerRegistry.Lock()
	defer providerRegistry.Unlock()

	if _, exists := providerRegistry.ctors[name]; exists {
		panic(fmt.Sprintf("einoproviders: RegisterProvider called twice for %q", name))
	}
	providerRegistry.ctors[name] = ctor
}

func lookupProvider(name string) (ProviderCtor, bool) {
	providerRegistry.RLock()
	defer providerRegistry.RUnlock()

	ctor, ok := providerRegistry.ctors[name]
	return ctor, ok
}
