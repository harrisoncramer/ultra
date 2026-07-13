package compose

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Composer renders compose overrides and the namespaced launcher variable names.
type Composer struct{}

// NewComposer returns a Composer. It holds no state; the type exists so callers
// can depend on it as an injected interface.
func NewComposer() *Composer { return &Composer{} }

// Var returns the app-namespaced launcher variable an override maps a secret onto.
func (c *Composer) Var(app, name string) string {
	return ComposeVar(app, name)
}

// Override renders the single compose file that maps every app's secrets onto
// their namespaced launcher variables. It contains references only, never values.
func (c *Composer) Override(apps []AppSecrets) string {
	return ComposeOverride(apps)
}

// ServiceNames reads the top-level service names declared in a docker compose
// file. It parses the YAML directly rather than shelling out to docker, so it
// stays offline like the rest of generation. It is used to scope a generated
// file to only the services a given compose stack defines.
func ServiceNames(path string) (map[string]bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading compose file %s: %w", path, err)
	}
	var doc struct {
		Services map[string]yaml.Node `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parsing compose file %s: %w", path, err)
	}
	names := make(map[string]bool, len(doc.Services))
	for name := range doc.Services {
		names[name] = true
	}
	return names, nil
}
