package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/harrisoncramer/ultra/internal/resolve"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// configFileName is the optional TOML file, read from the repo root, that
// prebakes command-line flags. Anything passed on the command line overrides it.
//
// It mirrors the CLI's hierarchy: a section picks a resolver, and that resolver's
// own flags live in a sub-table keyed by the resolver's name, so flags never
// leak across resolvers.
//
//	[secrets]
//	resolver = "aws-secret-manager"   # --secret-resolver
//
//	[secrets.aws-secret-manager]      # aws-secret-manager's own flags
//	region  = "us-east-1"
//	profile = "prod"
//
//	[config]
//	resolver = "docker-compose"       # --config-resolver
//
//	# top-level keys map to the shared flags
//	apps-dir = "services"             # --apps-dir
//
//	# optional override layers: any registered resolver, layered on top of the
//	# base one, whose values win. Their flags live only here, never on the command
//	# line, so a same-provider override doesn't collide with the base resolver.
//	[secrets-override]
//	resolver = "1password"
//	[secrets-override.1password]
//	vault = "LocalDev"
//
//	[config-override]
//	resolver = "env"
const configFileName = ".ultra.toml"

// fileConfig holds the defaults read from .ultra.toml: flag values keyed by flag
// name (for example "secret-resolver", "region"), plus the default list of app
// paths to operate on when none are passed on the command line. Both are empty
// when no file exists.
type fileConfig struct {
	flags    map[string]string
	apps     []string
	override overrideConfig
}

// overrideConfig holds the optional resolvers layered on top of the base ones,
// read from the [secrets-override] and [config-override] sections. Each keeps its
// own resolver's flags, separate from the base resolver's, so a same-provider
// override does not clobber them.
type overrideConfig struct {
	secretResolver string
	secretFlags    map[string]string
	configResolver string
	configFlags    map[string]string
}

// secretOverride builds the override secret resolver factory the file configures,
// or nil when none is set.
func (fc fileConfig) secretOverride() func(app string) resolve.SecretResolver {
	return resolve.BuildSecretOverride(fc.override.secretResolver, fc.override.secretFlags)
}

// configOverride builds the override config resolver the file configures for
// root, or nil when none is set.
func (fc fileConfig) configOverride(root string) (resolve.ConfigResolver, error) {
	return resolve.BuildConfigOverride(fc.override.configResolver, fc.override.configFlags, root)
}

// loadConfig reads the ultra config file and flattens its sections into flag
// defaults. The path is taken from --config-file when given, otherwise it defaults
// to configFileName under the repo root, which is located via a raw scan for
// --root (defaulting to the current directory since the file itself can set root).
// Both flags are read raw from os.Args because the file is loaded before cobra
// parses. A missing file yields an empty config; a malformed file is an error.
func loadConfig() (fileConfig, error) {
	path := rawFlagValue(os.Args, "config-file")
	explicit := path != ""
	if !explicit {
		root := rawFlagValue(os.Args, "root")
		if root == "" {
			root = "."
		}
		path = filepath.Join(root, configFileName)
	}
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		missing := errors.As(err, &notFound) || errors.Is(err, os.ErrNotExist)
		if missing && !explicit {
			return fileConfig{}, nil
		}
		return fileConfig{}, fmt.Errorf("reading %s: %w", path, err)
	}
	return flatten(v), nil
}

// flatten maps the hierarchical TOML onto flag names: top-level scalar keys map
// to the shared flags (root, apps-dir), [secrets].resolver picks --secret-resolver
// and [secrets.<resolver>] holds that resolver's own flags, and likewise for
// [config]. Only the selected resolver's sub-table is read, so flags for other
// resolvers are ignored rather than colliding.
func flatten(v *viper.Viper) fileConfig {
	flags := map[string]string{}
	for k, val := range v.AllSettings() {
		if k == "apps" {
			continue
		}
		if _, isSection := val.(map[string]any); isSection {
			continue
		}
		flags[k] = fmt.Sprint(val)
	}
	applySection(v, flags, "secrets", "secret-resolver")
	applySection(v, flags, "config", "config-resolver")

	override := overrideConfig{secretFlags: map[string]string{}, configFlags: map[string]string{}}
	override.secretResolver = applyOverrideSection(v, override.secretFlags, "secrets-override")
	override.configResolver = applyOverrideSection(v, override.configFlags, "config-override")

	return fileConfig{flags: flags, apps: v.GetStringSlice("apps"), override: override}
}

// applyOverrideSection records the override resolver a section selects and copies
// that resolver's own flags out of its name-keyed sub-table into flags, returning
// the resolver name. Unlike applySection the command line never picks an override
// resolver, so the choice comes only from the file.
func applyOverrideSection(v *viper.Viper, flags map[string]string, section string) string {
	sub := v.Sub(section)
	if sub == nil {
		return ""
	}
	name := sub.GetString("resolver")
	if name == "" {
		return ""
	}
	if rs := sub.Sub(name); rs != nil {
		for k, val := range rs.AllSettings() {
			flags[k] = fmt.Sprint(val)
		}
	}
	return name
}

// applySection records the resolver chosen for a section and copies that
// resolver's own flags out of its name-keyed sub-table. The command line wins
// over the file when picking which resolver — and therefore which sub-table — to
// read, so an override still gets its matching defaults.
func applySection(v *viper.Viper, flags map[string]string, section, resolverFlag string) {
	sub := v.Sub(section)
	if sub == nil {
		return
	}
	name := rawFlagValue(os.Args, resolverFlag)
	if name == "" {
		name = sub.GetString("resolver")
	}
	if name == "" {
		return
	}
	flags[resolverFlag] = name
	if rs := sub.Sub(name); rs != nil {
		for k, val := range rs.AllSettings() {
			flags[k] = fmt.Sprint(val)
		}
	}
}

// effective returns the value for a flag before cobra has parsed: the command
// line wins, then the config file. Used to pick the secret resolver at build
// time, when its flags must already be bound.
func (fc fileConfig) effective(name string) string {
	if raw := rawFlagValue(os.Args, name); raw != "" {
		return raw
	}
	return fc.flags[name]
}

// applyConfigDefaults fills any flag the user did not pass on the command line
// with its .ultra.toml value, so the file acts as a set of defaults the command
// line overrides. Call after parsing, once every flag (including the selected
// resolver's) is bound.
func applyConfigDefaults(cmd *cobra.Command, fc fileConfig) error {
	var err error
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if err != nil || f.Changed {
			return
		}
		if v, ok := fc.flags[f.Name]; ok {
			if setErr := f.Value.Set(v); setErr != nil {
				err = fmt.Errorf("%s from %s: %w", f.Name, configFileName, setErr)
			}
		}
	})
	return err
}
