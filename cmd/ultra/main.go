package main

import (
	"context"
	"os"

	"github.com/harrisoncramer/ultra/cmd/ultra/flags"
	"github.com/harrisoncramer/ultra/cmd/ultra/resolvers"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().ExecuteContext(context.Background()); err != nil {
		os.Exit(1)
	}
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
	shared := &flags.SharedFlags{}

	cmd := &cobra.Command{
		Use:   "run <resolver> [flags] -- <command>...",
		Short: "Resolve every app's secrets with a resolver and exec the command",
		Long: "run takes a resolver as its subcommand (for example 1password), discovers\n" +
			"every app under the apps directory, resolves each app's secrets via that\n" +
			"resolver in memory, forwards them into that app's container through a generated\n" +
			"compose override, and execs the given command. No secret is written to disk.",
	}

	cmd.PersistentFlags().StringVar(&shared.Root, "root", ".", "repo root the compose file and overrides are anchored to")
	cmd.PersistentFlags().StringVar(&shared.AppsDir, "apps-dir", "apps", "directory under --root holding each app's config package")

	// Each resolver is its own subcommand of run. Register the aws resolver here.
	cmd.AddCommand(resolvers.NewOnePasswordCmd(shared))
	return cmd
}
