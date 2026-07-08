package cli

import "github.com/harrisoncramer/ultra/internal/resolve"

// The resolver API is re-exported from internal/resolve so a custom build can add
// its own backend: import this package, register a resolver, and call Execute.
// The types are aliases, so a resolver written against cli.SecretResolver is the
// exact type the internal domains consume.
type (
	// SecretResolver fetches secrets from a backing store in bulk.
	SecretResolver = resolve.SecretResolver
	// ConfigResolver fetches an app's non-secret configuration.
	ConfigResolver = resolve.ConfigResolver
	// SecretResolverCommand describes a secret resolver and how to build it.
	SecretResolverCommand = resolve.SecretResolverCommand
	// ConfigResolverCommand describes a config resolver and how to build it.
	ConfigResolverCommand = resolve.ConfigResolverCommand
)

// ErrSecretNotFound is returned by a secret resolver when the store has no entry
// for an app, letting the override layer fall through to the base resolver.
var ErrSecretNotFound = resolve.ErrSecretNotFound

// RegisterSecretResolver adds a secret resolver. Call before Execute.
func RegisterSecretResolver(rc SecretResolverCommand) {
	resolve.RegisterSecretResolver(rc)
}

// RegisterConfigResolver adds a config resolver. Call before Execute.
func RegisterConfigResolver(rc ConfigResolverCommand) {
	resolve.RegisterConfigResolver(rc)
}
