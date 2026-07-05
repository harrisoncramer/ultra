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
// It is organized into sections mirroring the two resolver kinds:
//
//	[secrets]
//	resolver = "aws-secret-manager"   # --secret-resolver
//	region   = "us-east-1"            # the resolver's own flag
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

// flatten maps the sectioned TOML onto flag names: [secrets].resolver becomes
// --secret-resolver, [config].resolver becomes --config-resolver, every other key
// in a section maps to the resolver flag of the same name (region, vault, …), and
// top-level scalar keys map to the shared flags (root, apps-dir).
func flatten(v *viper.Viper) fileConfig {
	fc := fileConfig{}
	for k, val := range v.AllSettings() {
		if _, isSection := val.(map[string]any); isSection {
			continue
		}
		fc[k] = fmt.Sprint(val)
	}

	section := func(name, resolverFlag string) {
		sub := v.Sub(name)
		if sub == nil {
			return
		}
		for k, val := range sub.AllSettings() {
			if k == "resolver" {
				fc[resolverFlag] = fmt.Sprint(val)
				continue
			}
			fc[k] = fmt.Sprint(val)
		}
	}
	section("secrets", "secret-resolver")
	section("config", "config-resolver")
	return fc
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
