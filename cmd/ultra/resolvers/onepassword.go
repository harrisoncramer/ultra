package resolvers

import (
	"fmt"

	"github.com/harrisoncramer/ultra/cmd/ultra/flags"
	"github.com/harrisoncramer/ultra/cmd/ultra/runner"
	"github.com/harrisoncramer/ultra/pkg/resolvers"

	"github.com/spf13/cobra"
)

// NewOnePasswordCmd builds the `1password` resolver subcommand of run. It reads
// each app's secrets from a vault item named after the app, one field per secret
// name, via the op CLI.
func NewOnePasswordCmd(shared *flags.SharedFlags) *cobra.Command {
	var vault string

	cmd := &cobra.Command{
		Use:   "1password --vault <vault> -- <command>...",
		Short: "Resolve secrets from 1Password via the op CLI",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dash := cmd.ArgsLenAtDash()
			if dash < 0 || dash >= len(args) {
				return fmt.Errorf("usage: ultra run 1password --vault <vault> -- <command>")
			}
			if vault == "" {
				return fmt.Errorf("1password requires --vault")
			}
			return runner.Run(cmd.Context(), runner.Params{
				Root:    shared.Root,
				AppsDir: shared.AppsDir,
				ResolverFor: func(app string) resolvers.Resolver {
					return resolvers.NewOnePasswordSecretResolver(resolvers.NewOnePasswordSecretResolverParams{
						Vault: vault,
						Item:  app,
					})
				},
				Command: args[dash:],
			})
		},
	}

	cmd.Flags().StringVar(&vault, "vault", "", "1password vault holding the secrets (required)")
	return cmd
}
