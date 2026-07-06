package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/pflag"
)

// ConfigResolverCommand registers a config resolver under a name selectable via
// --config-resolver. Setup binds the resolver's own flags on fs and returns a
// factory that builds the resolver from the repo root once those flags are
// parsed, mirroring SecretResolverCommand.
type ConfigResolverCommand struct {
	Name  string
	Short string
	Setup func(fs *pflag.FlagSet) func(root string) (ConfigResolver, error)
}

var configResolvers []ConfigResolverCommand

// RegisterConfigResolver adds a config resolver. The built-in resolvers register
// themselves; call this before Execute to add your own.
func RegisterConfigResolver(rc ConfigResolverCommand) {
	configResolvers = append(configResolvers, rc)
}

func init() {
	RegisterConfigResolver(ConfigResolverCommand{
		Name:  "docker-compose",
		Short: "Read non-secret config from docker-compose.yml",
		Setup: func(fs *pflag.FlagSet) func(root string) (ConfigResolver, error) {
			return func(root string) (ConfigResolver, error) {
				return &dockerComposeConfig{composeFile: filepath.Join(root, "docker-compose.yml")}, nil
			}
		},
	})
	RegisterConfigResolver(ConfigResolverCommand{
		Name:  "env",
		Short: "Use the process environment (non-secrets already set, e.g. in a container or pod)",
		Setup: func(fs *pflag.FlagSet) func(root string) (ConfigResolver, error) {
			return func(root string) (ConfigResolver, error) {
				return envConfig{}, nil
			}
		},
	})
}

// findConfigResolver returns the config resolver command registered under name.
func findConfigResolver(name string) (ConfigResolverCommand, bool) {
	for _, rc := range configResolvers {
		if rc.Name == name {
			return rc, true
		}
	}
	return ConfigResolverCommand{}, false
}

func configResolverNames() string {
	names := make([]string, len(configResolvers))
	for i, rc := range configResolvers {
		names[i] = rc.Name
	}
	return strings.Join(names, ", ")
}

// dockerComposeConfig reads apps' non-secret environment from a docker-compose
// file via `docker compose config`. The file is read once and cached across apps.
type dockerComposeConfig struct {
	composeFile string

	once     sync.Once
	services map[string]map[string]string
	err      error
}

func (d *dockerComposeConfig) Resolve(ctx context.Context, app string) (map[string]string, error) {
	d.once.Do(func() { d.services, d.err = d.load(ctx) })
	if d.err != nil {
		return nil, d.err
	}
	return d.services[app], nil
}

func (d *dockerComposeConfig) load(ctx context.Context) (map[string]map[string]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", d.composeFile, "config", "--format", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("docker compose config: %s", msg)
	}

	var cfg struct {
		Services map[string]struct {
			Environment map[string]*string `json:"environment"`
		} `json:"services"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &cfg); err != nil {
		return nil, fmt.Errorf("parsing docker compose config: %w", err)
	}

	out := make(map[string]map[string]string, len(cfg.Services))
	for name, svc := range cfg.Services {
		env := make(map[string]string, len(svc.Environment))
		for k, v := range svc.Environment {
			if v != nil {
				env[k] = *v
			}
		}
		out[name] = env
	}
	return out, nil
}

// envConfig provides no non-secret values: the environment (a running container
// or pod) already holds them, and validate starts from the process env anyway.
type envConfig struct{}

func (envConfig) Resolve(ctx context.Context, app string) (map[string]string, error) {
	return map[string]string{}, nil
}
