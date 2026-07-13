package cli

import "github.com/harrisoncramer/ultra/internal/resolve"

// The resolver API is re-exported from internal/resolve so a custom build can add
// its own backend: import this package, register a resolver, and call Execute.
// The types are aliases, so a resolver written against cli.SecretResolver is the
// exact type the internal domains consume.
type (
	SecretResolver        = resolve.SecretResolver
	ConfigResolver        = resolve.ConfigResolver
	SecretResolverCommand = resolve.SecretResolverCommand
	ConfigResolverCommand = resolve.ConfigResolverCommand
)

var ErrSecretNotFound = resolve.ErrSecretNotFound

// RegisterSecretResolver adds a secret resolver. Call before Execute.
func RegisterSecretResolver(rc SecretResolverCommand) {
	resolve.RegisterSecretResolver(rc)
}

// RegisterConfigResolver adds a config resolver. Call before Execute.
func RegisterConfigResolver(rc ConfigResolverCommand) {
	resolve.RegisterConfigResolver(rc)
}
