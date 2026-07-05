package runner

import "context"

// Resolver fetches secrets from a backing store in bulk. Each resolver
// subcommand supplies an implementation via Params.ResolverFor, so the runner
// never depends on where secrets live.
type Resolver interface {
	// Resolve fetches values for names in a single round-trip and returns a map
	// keyed by name. A name the store doesn't have is simply omitted from the map
	// (missing individual secrets are surfaced later, at config load). A non-nil
	// error means the store itself is unreachable — e.g. the vault or item does
	// not exist — and is fatal.
	Resolve(ctx context.Context, names []string) (map[string]string, error)
}
