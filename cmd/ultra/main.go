// Command ultra wires every app's secrets — declared in each app's config.go via
// `secret:"true"` tags — into local docker-compose development, without ever
// writing secret values to disk.
//
//	ultra run -- docker compose up ...
//
// For every app under apps/ that declares secrets it: generates a names-only
// compose override that forwards those env vars into the container, resolves the
// values via the chosen resolver in memory, points compose at the overrides via
// COMPOSE_FILE, and execs the given command with the secrets in its environment.
//
// The resolver is swappable (1Password now, others behind the same interface),
// so where secrets come from is a local-dev detail. In production the deploy
// platform injects the same env vars; nothing here runs there. Each app's
// config.go is the single source of truth for which secrets it needs.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/harrisoncramer/ultra"

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
	var root, resolverKind, vault string

	cmd := &cobra.Command{
		Use:   "run -- <command>...",
		Short: "Resolve every app's secrets and exec the command with them (e.g. docker compose up)",
		Long: "run discovers every app under apps/, generates a compose override forwarding\n" +
			"each app's secret env vars, resolves the values via the chosen resolver in\n" +
			"memory, and execs the given command with the secrets in its environment and\n" +
			"COMPOSE_FILE pointed at the overrides. No secret is ever written to disk.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dash := cmd.ArgsLenAtDash()
			if dash < 0 || dash >= len(args) {
				return fmt.Errorf("usage: ultra run -- <command>...")
			}
			return run(cmd.Context(), runParams{
				root:         root,
				resolverKind: resolverKind,
				vault:        vault,
				command:      args[dash:],
			})
		},
	}

	cmd.Flags().StringVar(&root, "root", ".", "repo root containing apps/<app>")
	cmd.Flags().StringVar(&resolverKind, "resolver", "1password", "secret resolver backend: "+strings.Join(resolverKinds, ", "))
	cmd.Flags().StringVar(&vault, "onepassword-vault", "", "1password vault holding the secrets (required for the 1password resolver)")
	return cmd
}

// resolverKinds lists the resolver backends the --resolver flag accepts.
var resolverKinds = []string{"1password"}

// newResolver builds the secret resolver named by kind for one app. Backend
// naming (which vault) is passed in; the app's secrets live in a vault item
// named after the app.
func newResolver(kind, vault, app string) (ultra.Resolver, error) {
	switch kind {
	case "1password":
		if vault == "" {
			return nil, fmt.Errorf("the 1password resolver requires --onepassword-vault")
		}
		return ultra.NewOnePasswordSecretResolver(ultra.NewOnePasswordSecretResolverParams{
			Vault: vault,
			Item:  app,
		}), nil
	default:
		return nil, fmt.Errorf("unknown resolver %q (valid: %s)", kind, strings.Join(resolverKinds, ", "))
	}
}

type runParams struct {
	root         string
	resolverKind string
	vault        string
	command      []string
}

func run(ctx context.Context, p runParams) error {
	apps, err := discoverApps(p.root)
	if err != nil {
		return err
	}

	env := os.Environ()
	composeFiles := []string{filepath.Join(p.root, "docker-compose.yml")}

	for _, app := range apps {
		names, err := ultra.SecretNames(configDir(p.root, app))
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

		resolver, err := newResolver(p.resolverKind, p.vault, app)
		if err != nil {
			return err
		}
		// One round-trip per app. A missing vault/item is fatal here; a missing
		// individual secret is not — that is surfaced by config.Load in the app,
		// the place the secret is actually read into the process.
		values, err := resolver.Resolve(ctx, names)
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

// discoverApps returns the names of every directory under <root>/apps that has a
// config package.
func discoverApps(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "apps"))
	if err != nil {
		return nil, fmt.Errorf("listing apps: %w", err)
	}
	var apps []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if info, err := os.Stat(configDir(root, e.Name())); err == nil && info.IsDir() {
			apps = append(apps, e.Name())
		}
	}
	return apps, nil
}

func configDir(root, app string) string {
	return filepath.Join(root, "apps", app, "config")
}
