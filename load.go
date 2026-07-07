package ultra

import (
	"fmt"
	"strings"

	"github.com/caarlos0/env/v10"
)

// Load parses the environment into cfg and returns it, erroring if anything
// required is missing or malformed. Apps depend on this instead of the env
// library directly, so config parsing, and the underlying dependency, is
// controlled in one place. Fields tagged `secret:"true"` are read here like any
// other; required ones fail the parse when unset.
//
// Pass WithEnvironment to declare the environment cfg is loaded for. A field
// tagged `envScope:"a,b"` is then required only when that environment is a or b,
// and ignored otherwise — so one Config can describe every environment and each
// environment enforces only its own required fields.
func Load[T any](cfg *T, opts ...Option) (*T, error) {
	o := newLoadOptions(opts)

	if conflicts := scopeConflicts(cfg); len(conflicts) > 0 {
		return nil, fmt.Errorf("failed to load config: %s combine envScope with required/notEmpty in the env tag; envScope already makes a field required within its environments", strings.Join(conflicts, ", "))
	}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	if missing := missingScopedRequired(cfg, o.environment); len(missing) > 0 {
		return nil, fmt.Errorf("failed to load config: environment %q requires %s to be set", o.environment, strings.Join(missing, ", "))
	}
	return cfg, nil
}
