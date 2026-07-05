package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// Execute builds the ultra command tree — root, run, validate — and runs it.
// Flags default to any values set in .ultra.toml at the repo root; the command
// line overrides them.
func Execute() error {
	fc, err := loadConfig()
	if err != nil {
		return err
	}
	return newRootCmd(fc).ExecuteContext(context.Background())
}

type sharedFlags struct {
	root    string
	appsDir string
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
		Use:   "run --secret-resolver <name> [flags] -- <command>...",
		Short: "Resolve every app's secrets with a secret resolver and exec the command",
		Long: "run resolves each app's secrets via the secret resolver named by\n" +
			"--secret-resolver (for example 1password), forwards them into that app's\n" +
			"container through a generated compose override, and execs the given command.\n" +
			"No secret is written to disk.",
		Args: cobra.MinimumNArgs(1),
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
			return fmt.Errorf("usage: ultra run --secret-resolver <name> [flags] -- <command>")
		}
		return run(cmd.Context(), runParams{
			root:        shared.root,
			appsDir:     shared.appsDir,
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
		Use:   "validate --secret-resolver <name> [flags]",
		Short: "Resolve every app's secrets and config and validate each app's Load",
		Long: "validate resolves secrets the same way as run (--secret-resolver), but rather\n" +
			"than starting containers it reconstructs the environment each app would boot\n" +
			"with — its non-secret config from --config-resolver (docker-compose by default)\n" +
			"plus its resolved secrets — and checks that the app's config.Load succeeds. It\n" +
			"reports each app and exits non-zero if any fail: a pre-flight before up.",
		Args: cobra.NoArgs,
	}
	addSharedFlags(cmd, shared)
	cmd.Flags().StringVar(&secretResolver, "secret-resolver", "", "secret backend: "+secretResolverNames())
	cmd.Flags().StringVar(&configResolver, "config-resolver", "docker-compose", "non-secret config source: "+configResolverNames())
	resolverFor := bindSelectedSecretResolver(cmd, fc)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := applyConfigDefaults(cmd, fc); err != nil {
			return err
		}
		if resolverFor == nil {
			return fmt.Errorf("--secret-resolver must be one of: %s", secretResolverNames())
		}
		cr, err := newConfigResolver(configResolver, shared.root)
		if err != nil {
			return err
		}
		return validate(cmd.Context(), validateParams{
			root:           shared.root,
			appsDir:        shared.appsDir,
			secretResolver: resolverFor,
			configResolver: cr,
		})
	}
	return cmd
}

func addSharedFlags(cmd *cobra.Command, shared *sharedFlags) {
	cmd.Flags().StringVar(&shared.root, "root", ".", "repo root the compose file and overrides are anchored to")
	cmd.Flags().StringVar(&shared.appsDir, "apps-dir", "apps", "directory under --root holding each app's config package")
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
