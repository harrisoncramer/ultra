package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

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
const configFileName = ".ultra.toml"

// fileConfig holds the flag defaults read from .ultra.toml, keyed by flag name
// (for example "secret-resolver", "region"). It is empty when no file exists.
type fileConfig map[string]string

// loadConfig reads .ultra.toml from the repo root and flattens its sections into
// flag defaults. The root is located via a raw scan for --root, defaulting to the
// current directory since the file itself can set root. A missing file yields an
// empty config; a malformed file is an error.
func loadConfig() (fileConfig, error) {
	root := rawFlagValue(os.Args, "root")
	if root == "" {
		root = "."
	}
	v := viper.New()
	v.SetConfigFile(filepath.Join(root, configFileName))
	v.SetConfigType("toml")
	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) || errors.Is(err, os.ErrNotExist) {
			return fileConfig{}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", configFileName, err)
	}
	return flatten(v), nil
}

// flatten maps the hierarchical TOML onto flag names: top-level scalar keys map
// to the shared flags (root, apps-dir), [secrets].resolver picks --secret-resolver
// and [secrets.<resolver>] holds that resolver's own flags, and likewise for
// [config]. Only the selected resolver's sub-table is read, so flags for other
// resolvers are ignored rather than colliding.
func flatten(v *viper.Viper) fileConfig {
	fc := fileConfig{}
	for k, val := range v.AllSettings() {
		if _, isSection := val.(map[string]any); isSection {
			continue
		}
		fc[k] = fmt.Sprint(val)
	}
	fc.applySection(v, "secrets", "secret-resolver")
	fc.applySection(v, "config", "config-resolver")
	return fc
}

// applySection records the resolver chosen for a section and copies that
// resolver's own flags out of its name-keyed sub-table. The command line wins
// over the file when picking which resolver — and therefore which sub-table — to
// read, so an override still gets its matching defaults.
func (fc fileConfig) applySection(v *viper.Viper, section, resolverFlag string) {
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
	fc[resolverFlag] = name
	if rs := sub.Sub(name); rs != nil {
		for k, val := range rs.AllSettings() {
			fc[k] = fmt.Sprint(val)
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
	return fc[name]
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
		if v, ok := fc[f.Name]; ok {
			if setErr := f.Value.Set(v); setErr != nil {
				err = fmt.Errorf("%s from %s: %w", f.Name, configFileName, setErr)
			}
		}
	})
	return err
}
