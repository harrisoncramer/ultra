// The resolver is a required subcommand of run (for example `1password`) and
// carries its own flags. For every app under the apps directory it: generates a
// names-only compose override that forwards those env vars into the container,
// resolves the values via the chosen resolver in memory, points compose at the
// overrides via COMPOSE_FILE, and execs the given command.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/harrisoncramer/ultra"
	"github.com/harrisoncramer/ultra/resolvers"

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

// sharedFlags are the flags every resolver subcommand inherits from run.
type sharedFlags struct {
	root    string
	appsDir string
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

	cmd.AddCommand(newOnePasswordCmd(shared))
	return cmd
}

func newOnePasswordCmd(shared *sharedFlags) *cobra.Command {
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
			resolverFor := func(app string) resolvers.Resolver {
				return resolvers.NewOnePasswordSecretResolver(resolvers.NewOnePasswordSecretResolverParams{
					Vault: vault,
					Item:  app,
				})
			}
			return run(cmd.Context(), runParams{
				root:        shared.root,
				appsDir:     shared.appsDir,
				resolverFor: resolverFor,
				command:     args[dash:],
			})
		},
	}

	cmd.Flags().StringVar(&vault, "vault", "", "1password vault holding the secrets (required)")
	return cmd
}

type runParams struct {
	root        string
	appsDir     string
	resolverFor func(app string) resolvers.Resolver
	command     []string
}

func run(ctx context.Context, p runParams) error {
	apps, err := discoverApps(p.root, p.appsDir)
	if err != nil {
		return err
	}

	env := os.Environ()
	composeFiles := []string{filepath.Join(p.root, "docker-compose.yml")}

	for _, app := range apps {
		names, err := ultra.SecretNames(configDir(p.root, p.appsDir, app))
		if err != nil {
			return fmt.Errorf("reading %s config: %w", app, err)
		}
		if len(names) == 0 {
			continue
		}

		override := filepath.Join(p.root, "tmp", app+".compose.yml")
		if err := os.MkdirAll(filepath.Dir(override), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(override, []byte(ultra.ComposeOverride(app, names)), 0o644); err != nil {
			return err
		}
		composeFiles = append(composeFiles, override)

		// One round-trip per app. A missing vault/item is fatal here; a missing
		// individual secret is not.
		values, err := p.resolverFor(app).Resolve(ctx, names)
		if err != nil {
			return err
		}
		// Inject each secret under an app-namespaced key so two apps sharing a
		// name (e.g. DATABASE_URL) never collide in this shared environment. The
		// override maps the real name back per container via interpolation.
		for name, val := range values {
			env = append(env, ultra.ComposeVar(app, name)+"="+val)
		}
		fmt.Fprintf(os.Stderr, "ultra: resolved %d/%d secrets for %s\n", len(values), len(names), app)
	}

	env = append(env, "COMPOSE_FILE="+strings.Join(composeFiles, string(os.PathListSeparator)))

	bin, err := exec.LookPath(p.command[0])
	if err != nil {
		return fmt.Errorf("command not found: %s", p.command[0])
	}
	c := exec.CommandContext(ctx, bin, p.command[1:]...)
	c.Env = env
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// discoverApps returns the names of every directory under <root>/<appsDir> that
// has a config package.
func discoverApps(root, appsDir string) ([]string, error) {
	dir := filepath.Join(root, appsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("listing apps in %s: %w", dir, err)
	}
	var apps []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if info, err := os.Stat(configDir(root, appsDir, e.Name())); err == nil && info.IsDir() {
			apps = append(apps, e.Name())
		}
	}
	return apps, nil
}

func configDir(root, appsDir, app string) string {
	return filepath.Join(root, appsDir, app, "config")
}
