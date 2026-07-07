package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Execute builds the ultra command tree — root, run, validate — and runs it.
// Flags default to any values set in the ultra config file (.ultra.toml under
// --root, or the path given by --config-file); the command line overrides them.
func Execute() error {
	fc, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return err
	}
	return newRootCmd(fc).ExecuteContext(context.Background())
}

type sharedFlags struct {
	root       string
	configDir  string
	configFile string
}

func newRootCmd(fc fileConfig) *cobra.Command {
	root := &cobra.Command{
		Use:           "ultra",
		Short:         "Wire every app's config.go secrets into local docker-compose dev",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(newRunCmd(fc))
	root.AddCommand(newValidateCmd(fc))
	return root
}

func newRunCmd(fc fileConfig) *cobra.Command {
	shared := &sharedFlags{}
	var secretResolver string

	cmd := &cobra.Command{
		Use:   "run [app-path...] --secret-resolver <name> [flags] -- <command>...",
		Short: "Resolve the given apps' secrets with a secret resolver and exec the command",
		Long: "run resolves each app's secrets via the secret resolver named by\n" +
			"--secret-resolver (for example 1password), forwards them into that app's\n" +
			"container through a generated compose override, and execs the given command.\n" +
			"Apps are the directories given before --, each holding a config package (name\n" +
			"taken from the path's last element); if none are given the apps listed in\n" +
			".ultra.toml are used. No secret is written to disk.",
		Args: cobra.ArbitraryArgs,
	}
	addSharedFlags(cmd, shared)
	cmd.Flags().StringVar(&secretResolver, "secret-resolver", "", "secret backend: "+secretResolverNames())
	resolverFor := bindSelectedSecretResolver(cmd, fc)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := applyConfigDefaults(cmd, fc); err != nil {
			return err
		}
		if resolverFor == nil {
			return fmt.Errorf("--secret-resolver must be one of: %s", secretResolverNames())
		}
		dash := cmd.ArgsLenAtDash()
		if dash < 0 || dash >= len(args) {
			return fmt.Errorf("usage: ultra run [app-path...] -- <command>")
		}
		apps := resolveApps(args[:dash], fc)
		if len(apps) == 0 {
			return fmt.Errorf("no apps given: pass app paths before -- or set apps in .ultra.toml")
		}
		return run(cmd.Context(), runParams{
			root:        shared.root,
			apps:        apps,
			configDir:   shared.configDir,
			resolverFor: resolverFor,
			command:     args[dash:],
		})
	}
	return cmd
}

func newValidateCmd(fc fileConfig) *cobra.Command {
	shared := &sharedFlags{}
	var secretResolver, configResolver string

	cmd := &cobra.Command{
		Use:   "validate [app-path...] --secret-resolver <name> [flags]",
		Short: "Resolve the given apps' secrets and config and validate each app's Config",
		Long: "validate resolves secrets the same way as run (--secret-resolver), but rather\n" +
			"than starting containers it reconstructs the environment each app would boot\n" +
			"with — its non-secret config from --config-resolver (docker-compose by default)\n" +
			"plus its resolved secrets — and checks that ultra.Load parses the app's Config.\n" +
			"Apps are the directories given as arguments, or those listed in .ultra.toml\n" +
			"when none are given. It reports each app and exits non-zero if any fail.",
		Args: cobra.ArbitraryArgs,
	}
	addSharedFlags(cmd, shared)
	cmd.Flags().StringVar(&secretResolver, "secret-resolver", "", "secret backend: "+secretResolverNames())
	cmd.Flags().StringVar(&configResolver, "config-resolver", "docker-compose", "non-secret config source: "+configResolverNames())
	resolverFor := bindSelectedSecretResolver(cmd, fc)
	configResolverFor := bindSelectedConfigResolver(cmd, fc)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := applyConfigDefaults(cmd, fc); err != nil {
			return err
		}
		if resolverFor == nil {
			return fmt.Errorf("--secret-resolver must be one of: %s", secretResolverNames())
		}
		if configResolverFor == nil {
			return fmt.Errorf("--config-resolver must be one of: %s", configResolverNames())
		}
		apps := resolveApps(args, fc)
		if len(apps) == 0 {
			return fmt.Errorf("no apps given: pass app paths or set apps in .ultra.toml")
		}
		cr, err := configResolverFor(shared.root)
		if err != nil {
			return err
		}
		return validate(cmd.Context(), validateParams{
			root:           shared.root,
			apps:           apps,
			configDir:      shared.configDir,
			secretResolver: resolverFor,
			configResolver: cr,
		})
	}
	return cmd
}

func addSharedFlags(cmd *cobra.Command, shared *sharedFlags) {
	cmd.Flags().StringVar(&shared.root, "root", ".", "repo root the compose file and overrides are anchored to")
	cmd.Flags().StringVar(&shared.configDir, "config-dir", "config", "config package directory under each app path (e.g. pkg/config)")
	cmd.Flags().StringVar(&shared.configFile, "config-file", "", "path to the ultra config file (default "+configFileName+" under --root)")
}

// resolveApps returns the app paths to operate on: the given positional args
// (each also split on commas), or the apps listed in .ultra.toml when none are
// passed on the command line.
func resolveApps(args []string, fc fileConfig) []string {
	if len(args) == 0 {
		return fc.apps
	}
	var apps []string
	for _, a := range args {
		for _, p := range strings.Split(a, ",") {
			if p = strings.TrimSpace(p); p != "" {
				apps = append(apps, p)
			}
		}
	}
	return apps
}

// bindSelectedSecretResolver binds the flags of the resolver named by
// --secret-resolver onto cmd, so only that resolver's flags are defined — no
// prefixing, no collisions between resolvers — and returns its factory. The name
// comes from the command line or, failing that, .ultra.toml, since the resolver's
// flags must be bound before cobra parses.
func bindSelectedSecretResolver(cmd *cobra.Command, fc fileConfig) func(app string) SecretResolver {
	name := fc.effective("secret-resolver")
	for _, rc := range secretResolvers {
		if rc.Name == name {
			return rc.Setup(cmd.Flags())
		}
	}
	return nil
}

// bindSelectedConfigResolver binds the flags of the resolver named by
// --config-resolver onto cmd and returns its factory, mirroring
// bindSelectedSecretResolver. The name comes from the command line or
// .ultra.toml, falling back to the default docker-compose resolver, since the
// resolver's flags must be bound before cobra parses.
func bindSelectedConfigResolver(cmd *cobra.Command, fc fileConfig) func(root string) (ConfigResolver, error) {
	name := fc.effective("config-resolver")
	if name == "" {
		name = "docker-compose"
	}
	if rc, ok := findConfigResolver(name); ok {
		return rc.Setup(cmd.Flags())
	}
	return nil
}

// rawFlagValue finds --name's value in args, supporting both "--name v" and
// "--name=v".
func rawFlagValue(args []string, name string) string {
	flag := "--" + name
	for i, a := range args {
		switch {
		case a == flag && i+1 < len(args):
			return args[i+1]
		case strings.HasPrefix(a, flag+"="):
			return strings.TrimPrefix(a, flag+"=")
		}
	}
	return ""
}

func secretResolverNames() string {
	names := make([]string, len(secretResolvers))
	for i, rc := range secretResolvers {
		names[i] = rc.Name
	}
	return strings.Join(names, ", ")
}
