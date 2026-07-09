package resolve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/pflag"
)

// ConfigResolverCommand registers a config resolver under a name selectable via
// --config-resolver. Setup binds the resolver's own flags on fs and returns a
// factory that builds the resolver from the repo root once those flags are
// parsed, mirroring SecretResolverCommand.
type ConfigResolverCommand struct {
	Name  string
	Short string
	Setup func(fs *pflag.FlagSet) func(root string) (ConfigResolver, error)
}

var configResolvers []ConfigResolverCommand

// RegisterConfigResolver adds a config resolver. The built-in resolvers register
// themselves; call this before Execute to add your own.
func RegisterConfigResolver(rc ConfigResolverCommand) {
	configResolvers = append(configResolvers, rc)
}

func init() {
	RegisterConfigResolver(ConfigResolverCommand{
		Name:  "docker-compose",
		Short: "Read non-secret config from a docker compose file",
		Setup: func(fs *pflag.FlagSet) func(root string) (ConfigResolver, error) {
			file := fs.String("compose-file", "docker-compose.yml", "docker compose file to read non-secret config from, relative to --root")
			return func(root string) (ConfigResolver, error) {
				return &dockerComposeConfig{composeFile: filepath.Join(root, *file)}, nil
			}
		},
	})
	RegisterConfigResolver(ConfigResolverCommand{
		Name:  "env",
		Short: "Use the process environment (non-secrets already set, e.g. in a container or pod)",
		Setup: func(_ *pflag.FlagSet) func(root string) (ConfigResolver, error) {
			return func(_ string) (ConfigResolver, error) {
				return envConfig{}, nil
			}
		},
	})
}

// FindConfigResolver returns the config resolver command registered under name.
func FindConfigResolver(name string) (ConfigResolverCommand, bool) {
	for _, rc := range configResolvers {
		if rc.Name == name {
			return rc, true
		}
	}
	return ConfigResolverCommand{}, false
}

// ConfigResolverNames lists the registered config resolver names for help text.
func ConfigResolverNames() string {
	names := make([]string, len(configResolvers))
	for i, rc := range configResolvers {
		names[i] = rc.Name
	}
	return strings.Join(names, ", ")
}

// SecretLeakChecker is an optional capability of a ConfigResolver: it reports
// which of the given secret names carry a literal value in the non-secret config
// source, so lint can flag a secret hardcoded where only non-secret config
// belongs. A resolver whose source can't express a hardcoded value (e.g. the
// process environment) need not implement it.
type SecretLeakChecker interface {
	LeakedSecrets(ctx context.Context, app string, names []string) ([]string, error)
}

// layeredConfigResolver queries base then layers override on top, so an override
// value wins over the base for the same key. A nil override is a no-op.
type layeredConfigResolver struct {
	base     ConfigResolver
	override ConfigResolver
}

// Resolve returns the base values with the override's merged on top.
func (l layeredConfigResolver) Resolve(ctx context.Context, app string) (map[string]string, error) {
	out, err := l.base.Resolve(ctx, app)
	if err != nil {
		return nil, err
	}
	if l.override == nil {
		return out, nil
	}
	ov, err := l.override.Resolve(ctx, app)
	if err != nil {
		return nil, fmt.Errorf("config override resolver: %w", err)
	}
	return mergeOver(out, ov), nil
}

// LeakedSecrets unions the leaked secrets the base and override report, so a
// secret hardcoded in either source is flagged. A resolver that doesn't
// implement SecretLeakChecker contributes nothing.
func (l layeredConfigResolver) LeakedSecrets(ctx context.Context, app string, names []string) ([]string, error) {
	seen := make(map[string]bool)
	var leaked []string
	for _, r := range []ConfigResolver{l.base, l.override} {
		lc, ok := r.(SecretLeakChecker)
		if !ok {
			continue
		}
		found, err := lc.LeakedSecrets(ctx, app, names)
		if err != nil {
			return nil, err
		}
		for _, n := range found {
			if !seen[n] {
				seen[n] = true
				leaked = append(leaked, n)
			}
		}
	}
	return leaked, nil
}

// LayerConfigResolver wraps base so the override's values win, or returns base
// unchanged when no override is configured.
func LayerConfigResolver(base, override ConfigResolver) ConfigResolver {
	if override == nil {
		return base
	}
	return layeredConfigResolver{base: base, override: override}
}

// BuildConfigOverride builds the override config resolver named by name for root,
// or returns nil when name is empty. Its flags come from flags, bound on a
// private flag set.
func BuildConfigOverride(name string, flags map[string]string, root string) (ConfigResolver, error) {
	if name == "" {
		return nil, nil
	}
	rc, ok := FindConfigResolver(name)
	if !ok {
		return nil, fmt.Errorf("config-override resolver %q is not registered", name)
	}
	fs := pflag.NewFlagSet("config-override", pflag.ContinueOnError)
	build := rc.Setup(fs)
	applyFlagSet(fs, flags)
	return build(root)
}

// dockerComposeConfig reads apps' non-secret environment from a docker-compose
// file via `docker compose config`. Each read is cached across apps; the raw,
// non-interpolated read used for leak detection is cached separately.
type dockerComposeConfig struct {
	composeFile string

	once     sync.Once
	services map[string]map[string]string
	err      error

	rawOnce     sync.Once
	rawServices map[string]map[string]string
	rawErr      error
}

// Resolve returns app's non-secret environment from the compose file, loading
// and caching the file on first use.
func (d *dockerComposeConfig) Resolve(ctx context.Context, app string) (map[string]string, error) {
	d.once.Do(func() { d.services, d.err = d.load(ctx, false) })
	if d.err != nil {
		return nil, d.err
	}
	return d.services[app], nil
}

// LeakedSecrets reports which of names carry a literal value in the compose file,
// a secret hardcoded in non-secret config rather than resolved from the store.
// It reads with --no-interpolate so an entry that forwards a variable (contains
// ${...}) reads as a reference and is not flagged, only a real pasted value is.
func (d *dockerComposeConfig) LeakedSecrets(ctx context.Context, app string, names []string) ([]string, error) {
	d.rawOnce.Do(func() { d.rawServices, d.rawErr = d.load(ctx, true) })
	if d.rawErr != nil {
		return nil, d.rawErr
	}
	return literalLeaks(d.rawServices[app], names), nil
}

// literalLeaks returns the names whose value in env is a literal: present,
// non-empty, and not a ${...} reference. A value that forwards a variable is a
// reference, not a hardcoded secret, so it is excluded.
func literalLeaks(env map[string]string, names []string) []string {
	var leaked []string
	for _, name := range names {
		if v, ok := env[name]; ok && v != "" && !strings.Contains(v, "${") {
			leaked = append(leaked, name)
		}
	}
	return leaked
}

// load runs `docker compose config` once and returns each service's environment
// keyed by service name. With noInterpolate set, values keep their ${...}
// references instead of being resolved, so a literal can be told from a forward.
func (d *dockerComposeConfig) load(ctx context.Context, noInterpolate bool) (map[string]map[string]string, error) {
	args := []string{"compose", "-f", d.composeFile, "config", "--format", "json"}
	if noInterpolate {
		args = append(args, "--no-interpolate")
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("docker compose config: %s", msg)
	}

	var cfg struct {
		Services map[string]struct {
			Environment map[string]composeScalar `json:"environment"`
		} `json:"services"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &cfg); err != nil {
		return nil, fmt.Errorf("parsing docker compose config: %w", err)
	}

	out := make(map[string]map[string]string, len(cfg.Services))
	for name, svc := range cfg.Services {
		env := make(map[string]string, len(svc.Environment))
		for k, v := range svc.Environment {
			if v.set {
				env[k] = v.value
			}
		}
		out[name] = env
	}
	return out, nil
}

// composeScalar is a docker-compose environment value. `docker compose config
// --format json` emits values in their native JSON type — string, number, bool,
// or null — so a plain *string can't receive `OTEL_SAMPLE_RATE: 1` or
// `OTEL_ENABLED: false`. It coerces any scalar to its string form (the value a
// container would see) and treats null as unset.
type composeScalar struct {
	value string
	set   bool
}

// UnmarshalJSON accepts a JSON string, number, bool, or null, storing the scalar
// as a string and leaving null unset.
func (c *composeScalar) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		c.value, c.set = s, true
		return nil
	}
	c.value, c.set = string(data), true
	return nil
}

// envConfig provides no non-secret values: the environment (a running container
// or pod) already holds them, and validate starts from the process env anyway.
type envConfig struct{}

// Resolve returns no values: the environment already holds the non-secret config.
func (envConfig) Resolve(_ context.Context, _ string) (map[string]string, error) {
	return map[string]string{}, nil
}
