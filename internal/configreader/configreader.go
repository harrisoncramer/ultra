// Package configreader is the shared source of truth for which secrets each app
// declares: it reads apps' Config packages and reports their secret env-var
// names. Both gen (which writes the compose override) and run (which resolves
// and injects those secrets) depend on it, so the two commands always agree on
// the declared secrets. Reading is static over each app's config package; it
// never touches the secret store and never writes a file.
package configreader

import (
	"fmt"
	"sort"

	"github.com/harrisoncramer/ultra/internal/compose"
	"github.com/harrisoncramer/ultra/internal/project"
)

// scanner reports the secret env-var names an app's Config declares.
type scanner interface {
	SecretNames(dir string) ([]string, error)
}

// AppOutput is one app's declared secret names, in sorted order. An app that
// declares no secrets has no Names.
type AppOutput struct {
	App   string
	Names []string
}

// ConfigReader reads apps' Config packages to report the secrets they declare.
type ConfigReader struct {
	scanner scanner
	project project.Project
}

// NewConfigReaderParams are the dependencies NewConfigReader needs.
type NewConfigReaderParams struct {
	Scanner scanner
	Project project.Project
}

// NewConfigReader builds a ConfigReader.
func NewConfigReader(params NewConfigReaderParams) *ConfigReader {
	return &ConfigReader{scanner: params.Scanner, project: params.Project}
}

// validEnvName reports whether name is a valid environment variable identifier,
// matching [A-Za-z_][A-Za-z0-9_]*. A secret whose name isn't can't be forwarded
// through a compose ${...} launcher variable, which accepts only this grammar.
func validEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r == '_':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// Read reads each app's Config and returns its declared secret names in input
// order, sorted within each app. It fails if two apps normalize to the same
// secret namespace or if a declared secret is not a valid env-var name. It
// reads only config, never the secret store, and writes nothing.
func (c *ConfigReader) Read(apps []string) ([]AppOutput, error) {
	out := make([]AppOutput, 0, len(apps))
	seen := make(map[string]string, len(apps))
	for _, appPath := range apps {
		app := c.project.AppName(appPath)
		// Key on the sanitized launcher namespace, not the raw name: apps whose
		// names differ only by characters that normalize to the same segment (e.g.
		// my-app and my_app both become MY_APP) map onto the same launcher
		// variables and would silently cross-contaminate each other's secrets.
		ns := compose.Namespace(app)
		if prev, dup := seen[ns]; dup {
			return nil, fmt.Errorf("apps %s and %s map to the same secret namespace %q: their secrets would collide, so rename one so the app names differ after normalization", prev, appPath, ns)
		}
		seen[ns] = appPath
		names, err := c.scanner.SecretNames(c.project.AppConfigDir(appPath))
		if err != nil {
			return nil, fmt.Errorf("reading %s config: %w", app, err)
		}
		if len(names) == 0 {
			out = append(out, AppOutput{App: app})
			continue
		}
		for _, name := range names {
			if !validEnvName(name) {
				return nil, fmt.Errorf("app %s declares secret %q, which is not a valid environment variable name ([A-Za-z_][A-Za-z0-9_]*) and cannot be forwarded through a compose ${...} variable", app, name)
			}
		}
		sort.Strings(names)
		out = append(out, AppOutput{App: app, Names: names})
	}
	return out, nil
}
