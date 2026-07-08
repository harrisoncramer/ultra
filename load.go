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
// other.
//
// Required-ness is declared with the required tag, not the env library's
// required/notEmpty options (Load rejects those). A field tagged `required:"*"`
// must be set and non-empty in every environment; `required:"a,b"` only in
// environments a or b, named via WithEnvironment; and an untagged field is never
// required. So one Config describes every environment and each enforces only its
// own required fields.
func Load[T any](cfg *T, opts ...Option) (*T, error) {
	o := newLoadOptions(opts)

	if bad := envTagRequiredFields(cfg); len(bad) > 0 {
		return nil, fmt.Errorf("failed to load config: %s declare required/notEmpty in the env tag; declare required-ness with the required tag instead (e.g. `required:\"*\"`)", strings.Join(bad, ", "))
	}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	if missing := missingRequired(cfg, o.environment); len(missing) > 0 {
		scope := ""
		if o.environment != "" {
			scope = fmt.Sprintf(" for environment %q", o.environment)
		}
		return nil, fmt.Errorf("failed to load config: %s required but not set%s", strings.Join(missing, ", "), scope)
	}
	return cfg, nil
}
