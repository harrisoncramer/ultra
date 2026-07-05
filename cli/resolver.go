// Package cli builds ultra's command tree and lets consumers extend it with
// their own resolvers. Import it, register a secret resolver of your own, and
// call Execute — the built-in resolvers come along, so you never fork ultra's
// cmd to add a backend.
//
// ultra has two symmetric resolver kinds. A SecretResolver says where secrets
// come from (1Password, AWS Secrets Manager, ...). A ConfigResolver says where an
// app's non-secret configuration comes from (docker-compose locally, the process
// env in a running container/pod, ...). validate combines both to reconstruct the
// full environment an app would boot with.
package cli

import (
	"context"

	"github.com/spf13/pflag"
)

// SecretResolver fetches secrets from a backing store in bulk. Implement it to
// add a new secret backend.
type SecretResolver interface {
	// Resolve fetches values for names in a single round-trip and returns a map
	// keyed by name. A name the store doesn't have is simply omitted from the map
	// (missing individual secrets are surfaced later, at config load). A non-nil
	// error means the store itself is unreachable — e.g. the vault does not exist
	// or credentials are missing — and is fatal.
	Resolve(ctx context.Context, names []string) (map[string]string, error)
}

// ConfigResolver fetches an app's non-secret configuration — the values the
// platform provides alongside the secrets ultra resolves.
type ConfigResolver interface {
	// Resolve returns the non-secret environment for app. A non-nil error means
	// the config source is unavailable and is fatal.
	Resolve(ctx context.Context, app string) (map[string]string, error)
}

// SecretResolverCommand describes a secret resolver exposed as a subcommand of
// `ultra run` and `ultra validate`. Setup binds the resolver's own flags on fs
// and returns a factory that builds a resolver for a given app once those flags
// are parsed.
type SecretResolverCommand struct {
	Name  string
	Short string
	Long  string
	Setup func(fs *pflag.FlagSet) func(app string) SecretResolver
}

var secretResolvers []SecretResolverCommand

// RegisterSecretResolver adds a secret resolver subcommand. The built-in
// resolvers register themselves; call this before Execute to add your own.
func RegisterSecretResolver(rc SecretResolverCommand) {
	secretResolvers = append(secretResolvers, rc)
}
