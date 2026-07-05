// Package resolvers defines the interface for fetching secrets from a backing
// store, along with the resolvers ultra ships with.
package resolvers

import "context"

type Resolver interface {
	// Resolve fetches values for names in a single round-trip and returns a map
	// keyed by name. A name the store doesn't have is simply omitted from the map
	// (missing individual secrets are surfaced later, at config load). A non-nil
	// error means the store itself is unreachable.
	Resolve(ctx context.Context, names []string) (map[string]string, error)
}
