package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

// Execute builds the ultra command tree — root, run, and every registered
// resolver subcommand — and runs it.
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
	return root
}

func newRunCmd() *cobra.Command {
	shared := &sharedFlags{}

	cmd := &cobra.Command{
		Use:   "run <resolver> [flags] -- <command>...",
		Short: "Resolve every app's secrets with a resolver and exec the command",
		Long: "run takes a resolver as its subcommand (for example 1password), discovers\n" +
			"every app under the apps directory, resolves each app's secrets via that\n" +
			"resolver in memory, forwards them into that app's container through a generated\n" +
			"compose override, and execs the given command. No secret is written to disk.",
	}

	cmd.PersistentFlags().StringVar(&shared.root, "root", ".", "repo root the compose file and overrides are anchored to")
	cmd.PersistentFlags().StringVar(&shared.appsDir, "apps-dir", "apps", "directory under --root holding each app's config package")

	for _, rc := range registry {
		cmd.AddCommand(resolverSubcmd(rc, shared))
	}
	return cmd
}

// resolverSubcmd turns a registered ResolverCommand into a cobra subcommand of
// run. Setup binds the resolver's flags now and returns the factory RunE uses
// once they're parsed.
func resolverSubcmd(rc ResolverCommand, shared *sharedFlags) *cobra.Command {
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
