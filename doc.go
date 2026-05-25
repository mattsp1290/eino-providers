// Package einoproviders provides shared CloudWeGo Eino provider construction
// for agentic applications.
//
// The root package defines the single-shot Provider contract, common Options,
// usage extraction, error classification, and a registry-backed NewProvider
// factory.
//
// Backend packages register themselves from init. Import only the backends a
// program needs, usually for side effects:
//
//	import (
//		einoproviders "github.com/mattsp1290/eino-providers"
//		_ "github.com/mattsp1290/eino-providers/claude"
//	)
//
// The root package intentionally does not import backend subpackages. This keeps
// backend SDKs and Codex OAuth dependencies opt-in.
//
// RegisterProvider is public so backend packages can self-register and tests can
// install local constructors. Direct third-party registration should be treated
// as experimental until the registry API is stabilized in a later minor release.
package einoproviders
