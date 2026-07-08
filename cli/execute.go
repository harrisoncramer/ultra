// Package cli builds ultra's command tree and wires the domain services
// together. It is the composition root: each command constructs the scanner,
// composer and resolvers once and injects them into the run, validate and lint
// services. Consumers extend ultra by registering their own resolvers (see
// resolvers.go) and calling Execute from their own main.
package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/harrisoncramer/ultra/internal/compose"
	"github.com/harrisoncramer/ultra/internal/gen"
	"github.com/harrisoncramer/ultra/internal/lint"
	"github.com/harrisoncramer/ultra/internal/project"
	"github.com/harrisoncramer/ultra/internal/resolve"
	"github.com/harrisoncramer/ultra/internal/run"
	"github.com/harrisoncramer/ultra/internal/scan"
	"github.com/harrisoncramer/ultra/internal/validate"

	"github.com/spf13/cobra"
)

// Execute builds the ultra command tree (root, run, validate, lint) and runs
// it. Flags default to any values set in the ultra config file (.ultra.toml under
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
	root           string
	configDir      string
	configFile     string
	outputDir      string
	outputFilename string
}

func (s *sharedFlags) project() project.Project {
	return project.Project{Root: s.root, ConfigDir: s.configDir}
}

// DocsCommand builds the ultra command tree with no config-file defaults applied,
// for generating the command reference. Resolver-specific flags are bound only
// when a resolver is selected, so they are documented in the resolvers guide
// rather than here.
func DocsCommand() *cobra.Command {
	return newRootCmd(fileConfig{})
}

func newRootCmd(fc fileConfig) *cobra.Command {
	root := &cobra.Command{
		Use:           "ultra",
		Short:         "Wire every app's config.go secrets into local docker-compose dev",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(newGenCmd(fc))
	root.AddCommand(newRunCmd(fc))
	root.AddCommand(newValidateCmd(fc))
	root.AddCommand(newLintCmd(fc))
	return root
}

func newGenCmd(fc fileConfig) *cobra.Command {
	shared := &sharedFlags{}
	var composeFile string

	cmd := &cobra.Command{
		Use:   "gen [app-path...] [flags]",
		Short: "Generate the single compose file for the given apps without resolving secrets",
		Long: "gen writes one names-only docker compose file, mapping every secret each app's\n" +
			"Config declares onto its namespaced launcher variable, one service block per\n" +
			"app, into --output-dir/--output-filename. It reads only the apps' config\n" +
			"packages and never contacts the secret store, so it works offline and its\n" +
			"output can be committed and reused by run. Apps are the directories given as\n" +
			"arguments, or those listed in .ultra.toml when none are given.\n\n" +
			"Pass --compose-file to scope the output to one stack: gen then writes a service\n" +
			"block only for apps whose service the given compose file defines, so the result\n" +
			"merges cleanly onto a subset stack. Generate one file per stack this way.",
		Args: cobra.ArbitraryArgs,
	}
	addSharedFlags(cmd, shared)
	cmd.Flags().StringVar(&composeFile, "compose-file", "", "scope the output to the services this compose file defines, relative to --root (default: every app)")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := applyConfigDefaults(cmd, fc); err != nil {
			return err
		}
		apps := resolveApps(args, fc)
		if len(apps) == 0 {
			return fmt.Errorf("no apps given: pass app paths or set apps in .ultra.toml")
		}
		if composeFile != "" {
			scoped, err := scopeAppsToCompose(apps, filepath.Join(shared.root, composeFile), shared.project())
			if err != nil {
				return err
			}
			apps = scoped
		}
		result, err := gen.NewGenerator(gen.NewGeneratorParams{
			Scanner:        scan.NewScanner(),
			Composer:       compose.NewComposer(),
			Project:        shared.project(),
			OutputDir:      shared.outputDir,
			OutputFilename: shared.outputFilename,
		}).Generate(apps)
		if err != nil {
			return err
		}
		for _, o := range result.Apps {
			if len(o.Names) == 0 {
				fmt.Fprintf(os.Stderr, "ultra: %s declares no secrets, no service block written\n", o.App)
				continue
			}
			fmt.Fprintf(os.Stderr, "ultra: %s contributed %d secrets\n", o.App, len(o.Names))
		}
		if result.Path == "" {
			fmt.Fprintln(os.Stderr, "ultra: no app declares a secret, no file written")
			return nil
		}
		base := filepath.Join(shared.root, "docker-compose.yml")
		if composeFile != "" {
			base = filepath.Join(shared.root, composeFile)
		}
		chain := strings.Join([]string{base, result.Path}, string(os.PathListSeparator))
		fmt.Fprintf(os.Stderr, "ultra: wrote %s\n", result.Path)
		fmt.Fprintf(os.Stderr, "ultra: run compose with COMPOSE_FILE=%s (or docker compose -f %s -f %s)\n", chain, base, result.Path)
		return nil
	}
	return cmd
}

// scopeAppsToCompose keeps only the app paths whose service name the compose file
// at path defines, and reports each app it drops. It lets gen produce a file that
// merges cleanly onto a subset stack, such as a sandbox that runs fewer services.
func scopeAppsToCompose(apps []string, path string, proj project.Project) ([]string, error) {
	services, err := compose.ServiceNames(path)
	if err != nil {
		return nil, err
	}
	scoped := make([]string, 0, len(apps))
	for _, appPath := range apps {
		if services[proj.AppName(appPath)] {
			scoped = append(scoped, appPath)
			continue
		}
		fmt.Fprintf(os.Stderr, "ultra: %s has no service in %s, skipped\n", proj.AppName(appPath), path)
	}
	return scoped, nil
}

func newRunCmd(fc fileConfig) *cobra.Command {
	shared := &sharedFlags{}
	var secretResolver, composeFile string

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
	cmd.Flags().StringVar(&secretResolver, "secret-resolver", "", "secret backend: "+resolve.SecretResolverNames())
	cmd.Flags().StringVar(&composeFile, "compose-file", "docker-compose.yml", "base docker compose file COMPOSE_FILE points at, relative to --root")
	resolverFor := bindSelectedSecretResolver(cmd, fc)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := applyConfigDefaults(cmd, fc); err != nil {
			return err
		}
		if resolverFor == nil {
			return fmt.Errorf("--secret-resolver must be one of: %s", resolve.SecretResolverNames())
		}
		dash := cmd.ArgsLenAtDash()
		if dash < 0 || dash >= len(args) {
			return fmt.Errorf("usage: ultra run [app-path...] -- <command>")
		}
		apps := resolveApps(args[:dash], fc)
		if len(apps) == 0 {
			return fmt.Errorf("no apps given: pass app paths before -- or set apps in .ultra.toml")
		}
		runner := run.NewRunner(run.NewRunnerParams{
			Generator: gen.NewGenerator(gen.NewGeneratorParams{
				Scanner:        scan.NewScanner(),
				Composer:       compose.NewComposer(),
				Project:        shared.project(),
				OutputDir:      shared.outputDir,
				OutputFilename: shared.outputFilename,
			}),
			Composer:    compose.NewComposer(),
			Project:     shared.project(),
			ComposeFile: composeFile,
		})
		return runner.Run(cmd.Context(), run.Params{
			Apps:        apps,
			ResolverFor: resolve.LayerSecretResolver(resolverFor, fc.secretOverride()),
			Command:     args[dash:],
		})
	}
	return cmd
}

func newValidateCmd(fc fileConfig) *cobra.Command {
	shared := &sharedFlags{}
	var secretResolver, configResolver, environment string
	var rejectUnreferenced bool

	cmd := &cobra.Command{
		Use:   "validate [app-path...] --secret-resolver <name> [flags]",
		Short: "Resolve the given apps' secrets and config and validate each app's Config",
		Long: "validate resolves secrets the same way as run (--secret-resolver), but rather\n" +
			"than starting containers it reconstructs the environment each app would boot\n" +
			"with, its non-secret config from --config-resolver (docker-compose by default)\n" +
			"plus its resolved secrets, and checks that ultra.Load parses the app's Config.\n" +
			"Apps are the directories given as arguments, or those listed in .ultra.toml\n" +
			"when none are given. It reports each app and exits non-zero if any fail.",
		Args: cobra.ArbitraryArgs,
	}
	addSharedFlags(cmd, shared)
	cmd.Flags().StringVar(&secretResolver, "secret-resolver", "", "secret backend: "+resolve.SecretResolverNames())
	cmd.Flags().StringVar(&configResolver, "config-resolver", "docker-compose", "non-secret config source: "+resolve.ConfigResolverNames())
	cmd.Flags().StringVar(&environment, "env", "", "environment to check for; a field's required tag decides whether it's required in it")
	cmd.Flags().BoolVar(&rejectUnreferenced, "reject-unreferenced", false, "fail an app when a resolver provides a key no Config field references")
	resolverFor := bindSelectedSecretResolver(cmd, fc)
	configResolverFor := bindSelectedConfigResolver(cmd, fc)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := applyConfigDefaults(cmd, fc); err != nil {
			return err
		}
		if resolverFor == nil {
			return fmt.Errorf("--secret-resolver must be one of: %s", resolve.SecretResolverNames())
		}
		if configResolverFor == nil {
			return fmt.Errorf("--config-resolver must be one of: %s", resolve.ConfigResolverNames())
		}
		apps := resolveApps(args, fc)
		if len(apps) == 0 {
			return fmt.Errorf("no apps given: pass app paths or set apps in .ultra.toml")
		}
		cr, err := configResolverFor(shared.root)
		if err != nil {
			return err
		}
		overrideCR, err := fc.configOverride(shared.root)
		if err != nil {
			return err
		}
		validator := validate.NewValidator(validate.NewValidatorParams{
			Scanner:            scan.NewScanner(),
			Project:            shared.project(),
			Environment:        environment,
			RejectUnreferenced: rejectUnreferenced,
			SecretResolver:     resolve.LayerSecretResolver(resolverFor, fc.secretOverride()),
			ConfigResolver:     resolve.LayerConfigResolver(cr, overrideCR),
		})
		return validator.Validate(cmd.Context(), apps)
	}
	return cmd
}

func newLintCmd(fc fileConfig) *cobra.Command {
	shared := &sharedFlags{}
	var secretResolver, configResolver, environment string
	var rejectUnreferenced bool

	cmd := &cobra.Command{
		Use:   "lint [app-path...] --secret-resolver <name> [flags]",
		Short: "Statically check each app has no required key its resolvers won't provide",
		Long: "lint checks that every required config key an app declares is provided:\n" +
			"secrets by --secret-resolver, non-secret config by --config-resolver, by\n" +
			"comparing the declared keys against the keys those resolvers offer. Unlike\n" +
			"validate it never parses values or runs the app's config, so it works where\n" +
			"the real secret values aren't reachable, such as CI with a resolver that\n" +
			"reads declared keys from deployment manifests. Apps are the directories\n" +
			"given as arguments, or those in .ultra.toml when none are given. It reports\n" +
			"each app and exits non-zero if any required key is unprovided.",
		Args: cobra.ArbitraryArgs,
	}
	addSharedFlags(cmd, shared)
	cmd.Flags().StringVar(&secretResolver, "secret-resolver", "", "secret backend: "+resolve.SecretResolverNames())
	cmd.Flags().StringVar(&configResolver, "config-resolver", "docker-compose", "non-secret config source: "+resolve.ConfigResolverNames())
	cmd.Flags().StringVar(&environment, "env", "", "environment to check for; a field's required tag decides whether it's required in it")
	cmd.Flags().BoolVar(&rejectUnreferenced, "reject-unreferenced", false, "fail an app when a resolver provides a key no Config field references")
	resolverFor := bindSelectedSecretResolver(cmd, fc)
	configResolverFor := bindSelectedConfigResolver(cmd, fc)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := applyConfigDefaults(cmd, fc); err != nil {
			return err
		}
		if resolverFor == nil {
			return fmt.Errorf("--secret-resolver must be one of: %s", resolve.SecretResolverNames())
		}
		if configResolverFor == nil {
			return fmt.Errorf("--config-resolver must be one of: %s", resolve.ConfigResolverNames())
		}
		apps := resolveApps(args, fc)
		if len(apps) == 0 {
			return fmt.Errorf("no apps given: pass app paths or set apps in .ultra.toml")
		}
		cr, err := configResolverFor(shared.root)
		if err != nil {
			return err
		}
		overrideCR, err := fc.configOverride(shared.root)
		if err != nil {
			return err
		}
		linter := lint.NewLinter(lint.NewLinterParams{
			Scanner:            scan.NewScanner(),
			Project:            shared.project(),
			Environment:        environment,
			RejectUnreferenced: rejectUnreferenced,
			SecretResolver:     resolve.LayerSecretResolver(resolverFor, fc.secretOverride()),
			ConfigResolver:     resolve.LayerConfigResolver(cr, overrideCR),
		})
		return linter.Lint(cmd.Context(), apps)
	}
	return cmd
}

func addSharedFlags(cmd *cobra.Command, shared *sharedFlags) {
	cmd.Flags().StringVar(&shared.root, "root", ".", "repo root the compose file and overrides are anchored to")
	cmd.Flags().StringVar(&shared.configDir, "config-dir", "config", "config package directory under each app path (e.g. pkg/config)")
	cmd.Flags().StringVar(&shared.configFile, "config-file", "", "path to the ultra config file (default "+configFileName+" under --root)")
	cmd.Flags().StringVar(&shared.outputDir, "output-dir", "tmp", "directory under --root the generated compose file is written to; point it at a committed path to keep it in version control")
	cmd.Flags().StringVar(&shared.outputFilename, "output-filename", "ultra.compose.yml", "file name of the generated compose file under --output-dir; set it to docker-compose.override.yml to have compose auto-load it")
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
// --secret-resolver onto cmd, so only that resolver's flags are defined, with no
// prefixing, no collisions between resolvers, and returns its factory. The name
// comes from the command line or, failing that, .ultra.toml, since the resolver's
// flags must be bound before cobra parses.
func bindSelectedSecretResolver(cmd *cobra.Command, fc fileConfig) func(app string) resolve.SecretResolver {
	if rc, ok := resolve.FindSecretResolver(fc.effective("secret-resolver")); ok {
		return rc.Setup(cmd.Flags())
	}
	return nil
}

// bindSelectedConfigResolver binds the flags of the resolver named by
// --config-resolver onto cmd and returns its factory, mirroring
// bindSelectedSecretResolver. The name comes from the command line or
// .ultra.toml, falling back to the default docker-compose resolver.
func bindSelectedConfigResolver(cmd *cobra.Command, fc fileConfig) func(root string) (resolve.ConfigResolver, error) {
	name := fc.effective("config-resolver")
	if name == "" {
		name = "docker-compose"
	}
	if rc, ok := resolve.FindConfigResolver(name); ok {
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
