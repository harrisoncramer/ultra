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
	"fmt"

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

// layeredSecretResolver queries base then layers override on top, so an override
// value wins over the base for the same name. A nil override is a no-op.
type layeredSecretResolver struct {
	base     SecretResolver
	override SecretResolver
}

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
		return nil, fmt.Errorf("secret override resolver: %w", err)
	}
	if out == nil {
		out = make(map[string]string, len(ov))
	}
	for k, v := range ov {
		out[k] = v
	}
	return out, nil
}

// layerSecretResolver wraps base so the override's values win, or returns base
// unchanged when no override is configured.
func layerSecretResolver(base, override func(app string) SecretResolver) func(app string) SecretResolver {
	if override == nil {
		return base
	}
	return func(app string) SecretResolver {
		return layeredSecretResolver{base: base(app), override: override(app)}
	}
}

// buildSecretOverride returns the override secret resolver factory named by the
// [secrets-override] section, or nil when none is configured. Its flags come only
// from the file, bound on a private flag set so they never collide with the base
// resolver's flags on the command line.
func buildSecretOverride(fc fileConfig) func(app string) SecretResolver {
	name := fc.override.secretResolver
	if name == "" {
		return nil
	}
	for _, rc := range secretResolvers {
		if rc.Name != name {
			continue
		}
		fs := pflag.NewFlagSet("secret-override", pflag.ContinueOnError)
		factory := rc.Setup(fs)
		applyFlagSet(fs, fc.override.secretFlags)
		return factory
	}
	return nil
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
