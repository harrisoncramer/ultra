// Package cli builds ultra's command tree and lets consumers extend it with
// their own secret resolvers. Import it, RegisterResolver a resolver of your
// own, and call Execute — the built-in resolvers come along, so you never fork
// ultra's cmd to add a backend.
package cli

import (
	"context"

	"github.com/spf13/pflag"
)

// Resolver fetches secrets from a backing store in bulk. Implement it to add a
// new backend.
type Resolver interface {
	// Resolve fetches values for names in a single round-trip and returns a map
	// keyed by name. A name the store doesn't have is simply omitted from the map
	// (missing individual secrets are surfaced later, at config load). A non-nil
	// error means the store itself is unreachable — e.g. the vault does not exist
	// or credentials are missing — and is fatal.
	Resolve(ctx context.Context, names []string) (map[string]string, error)
}

// ResolverCommand describes a resolver exposed as a subcommand of `ultra run`.
// Setup binds the resolver's own flags on fs and returns a factory that builds a
// resolver for a given app once those flags are parsed.
type ResolverCommand struct {
	Name  string
	Short string
	Setup func(fs *pflag.FlagSet) func(app string) Resolver
}

var registry []ResolverCommand

// RegisterResolver adds a resolver subcommand to `ultra run`. The built-in
// resolvers register themselves; call this before Execute to add your own.
func RegisterResolver(rc ResolverCommand) {
	registry = append(registry, rc)
}
