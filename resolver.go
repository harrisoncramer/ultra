package ultra

import "context"

// Resolver fetches secrets from a backing store in bulk. Implementations are
// swappable — 1Password for local dev now, AWS Secrets Manager later — so the
// rest of the tooling never depends on where secrets live.
type Resolver interface {
	// Resolve fetches values for names in a single round-trip and returns a map
	// keyed by name. A name the store doesn't have is simply omitted from the map
	// (missing individual secrets are surfaced later, at config load). A non-nil
	// error means the store itself is unreachable — e.g. the vault or item does
	// not exist — and is fatal.
	Resolve(ctx context.Context, names []string) (map[string]string, error)
}
