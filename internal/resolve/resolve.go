// Package resolve defines ultra's secret and config resolver interfaces, the
// registry backends register into, and the layering that lets an override
// resolver win over a base one. The public cli package re-exports these types so
// custom builds can register their own resolvers.
package resolve

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/pflag"
)

// ErrSecretNotFound signals that a store has no entry for an app at all, as
// opposed to an auth or connectivity failure. A base resolver returning it is
// fatal, but the override layer treats it as "this override doesn't cover the
// app" and falls through to the base resolver, so an override only needs entries
// for the apps it actually shadows.
var ErrSecretNotFound = errors.New("secret not found")

// SecretResolver fetches secrets from a backing store in bulk. Implement it to
// add a new secret backend.
type SecretResolver interface {
	// Resolve fetches values for names in a single round-trip and returns a map
	// keyed by name. A name the store doesn't have is simply omitted from the map
	// (missing individual secrets are surfaced later, at config load). A non-nil
	// error means the store itself is unreachable (e.g. the vault does not exist
	// or credentials are missing) and is fatal; return an error wrapping
	// ErrSecretNotFound to say the store has no entry for this app so an override
	// can fall through to the base resolver.
	Resolve(ctx context.Context, names []string) (map[string]string, error)
}

// ConfigResolver fetches an app's non-secret configuration, the values the
// platform provides alongside the secrets ultra resolves.
type ConfigResolver interface {
	// Resolve returns the non-secret environment for app. A non-nil error means
	// the config source is unavailable and is fatal.
	Resolve(ctx context.Context, app string) (map[string]string, error)
}

// SecretResolverCommand describes a secret resolver exposed as a subcommand of
// ultra's commands. Setup binds the resolver's own flags on fs and returns a
// factory that builds a resolver for a given app once those flags are parsed.
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

// FindSecretResolver returns the secret resolver command registered under name.
func FindSecretResolver(name string) (SecretResolverCommand, bool) {
	for _, rc := range secretResolvers {
		if rc.Name == name {
			return rc, true
		}
	}
	return SecretResolverCommand{}, false
}

// SecretResolverNames lists the registered secret resolver names for help text.
func SecretResolverNames() string {
	names := make([]string, len(secretResolvers))
	for i, rc := range secretResolvers {
		names[i] = rc.Name
	}
	return strings.Join(names, ", ")
}

// layeredSecretResolver queries base then layers override on top, so an override
// value wins over the base for the same name. A nil override is a no-op.
type layeredSecretResolver struct {
	base     SecretResolver
	override SecretResolver
}

// Resolve returns the base values with the override's merged on top.
func (l layeredSecretResolver) Resolve(ctx context.Context, names []string) (map[string]string, error) {
	out, err := l.base.Resolve(ctx, names)
	if err != nil {
		return nil, err
	}
	if l.override == nil {
		return out, nil
	}
	ov, err := l.override.Resolve(ctx, names)
	if err != nil {
		if errors.Is(err, ErrSecretNotFound) {
			return out, nil
		}
		return nil, fmt.Errorf("secret override resolver: %w", err)
	}
	return mergeOver(out, ov), nil
}

// mergeOver returns base with override's entries layered on top so the override
// wins, tolerating a nil base.
func mergeOver(base, override map[string]string) map[string]string {
	if base == nil {
		base = make(map[string]string, len(override))
	}
	for k, v := range override {
		base[k] = v
	}
	return base
}

// LayerSecretResolver wraps base so the override's values win, or returns base
// unchanged when no override is configured.
func LayerSecretResolver(base, override func(app string) SecretResolver) func(app string) SecretResolver {
	if override == nil {
		return base
	}
	return func(app string) SecretResolver {
		return layeredSecretResolver{base: base(app), override: override(app)}
	}
}

// BuildSecretOverride returns the override secret resolver factory named by name,
// or nil when name is empty or unknown. Its flags come from flags, bound on a
// private flag set so they never collide with the base resolver's flags.
func BuildSecretOverride(name string, flags map[string]string) func(app string) SecretResolver {
	if name == "" {
		return nil
	}
	rc, ok := FindSecretResolver(name)
	if !ok {
		return nil
	}
	fs := pflag.NewFlagSet("secret-override", pflag.ContinueOnError)
	factory := rc.Setup(fs)
	applyFlagSet(fs, flags)
	return factory
}

// applyFlagSet sets every flag on fs whose name has a value in vals, used to feed
// an override resolver its flags from the config file.
func applyFlagSet(fs *pflag.FlagSet, vals map[string]string) {
	fs.VisitAll(func(f *pflag.Flag) {
		if v, ok := vals[f.Name]; ok {
			_ = f.Value.Set(v)
		}
	})
}
