package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// Execute builds the ultra command tree — root, run, validate, and every
// registered secret-resolver subcommand under each — and runs it.
func Execute() error {
	return newRootCmd().ExecuteContext(context.Background())
}

type sharedFlags struct {
	root    string
	appsDir string
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "ultra",
		Short:         "Wire every app's config.go secrets into local docker-compose dev",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(newRunCmd())
	root.AddCommand(newValidateCmd())
	return root
}

func newRunCmd() *cobra.Command {
	shared := &sharedFlags{}

	cmd := &cobra.Command{
		Use:   "run <secret-resolver> [flags] -- <command>...",
		Short: "Resolve every app's secrets with a secret resolver and exec the command",
		Long: "run takes a secret resolver as its subcommand (for example 1password),\n" +
			"discovers every app under the apps directory, resolves each app's secrets via\n" +
			"that resolver in memory, forwards them into that app's container through a\n" +
			"generated compose override, and execs the given command. No secret is written\n" +
			"to disk.",
	}
	addSharedFlags(cmd, shared)

	for _, rc := range secretResolvers {
		cmd.AddCommand(runSecretResolverCmd(rc, shared))
	}
	return cmd
}

func newValidateCmd() *cobra.Command {
	shared := &sharedFlags{}
	var configResolverKind string

	cmd := &cobra.Command{
		Use:   "validate <secret-resolver> [flags]",
		Short: "Resolve every app's secrets and config and validate each app's Load",
		Long: "validate takes the same secret-resolver subcommand and flags as run. Rather\n" +
			"than starting containers, it reconstructs the environment each app would boot\n" +
			"with — its non-secret config from the config resolver (docker-compose by\n" +
			"default) plus its resolved secrets — and checks that the app's config.Load\n" +
			"succeeds. It reports each app and exits non-zero if any fail: a pre-flight\n" +
			"before docker compose up.",
	}
	addSharedFlags(cmd, shared)
	cmd.PersistentFlags().StringVar(&configResolverKind, "config-resolver", "docker-compose",
		"where non-secret config comes from: "+configResolverNames())

	for _, rc := range secretResolvers {
		cmd.AddCommand(validateSecretResolverCmd(rc, shared, &configResolverKind))
	}
	return cmd
}

func addSharedFlags(cmd *cobra.Command, shared *sharedFlags) {
	cmd.PersistentFlags().StringVar(&shared.root, "root", ".", "repo root the compose file and overrides are anchored to")
	cmd.PersistentFlags().StringVar(&shared.appsDir, "apps-dir", "apps", "directory under --root holding each app's config package")
}

// runSecretResolverCmd turns a registered SecretResolverCommand into a subcommand
// of run. Setup binds the resolver's flags now and returns the factory RunE uses
// once they're parsed.
func runSecretResolverCmd(rc SecretResolverCommand, shared *sharedFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   rc.Name + " [flags] -- <command>...",
		Short: rc.Short,
		Long:  rc.Long,
		Args:  cobra.MinimumNArgs(1),
	}
	resolverFor := rc.Setup(cmd.Flags())

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		dash := cmd.ArgsLenAtDash()
		if dash < 0 || dash >= len(args) {
			return fmt.Errorf("usage: ultra run %s [flags] -- <command>", rc.Name)
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

// validateSecretResolverCmd turns a registered SecretResolverCommand into a
// subcommand of validate — same secret-resolver setup as run, paired with a
// config resolver to supply the non-secret values.
func validateSecretResolverCmd(rc SecretResolverCommand, shared *sharedFlags, configResolverKind *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   rc.Name + " [flags]",
		Short: rc.Short,
		Long:  rc.Long,
		Args:  cobra.NoArgs,
	}
	resolverFor := rc.Setup(cmd.Flags())

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		configResolver, err := newConfigResolver(*configResolverKind, shared.root)
		if err != nil {
			return err
		}
		return validate(cmd.Context(), validateParams{
			root:           shared.root,
			appsDir:        shared.appsDir,
			secretResolver: resolverFor,
			configResolver: configResolver,
		})
	}
	return cmd
}
