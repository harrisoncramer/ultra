package ultra

import (
	"fmt"
	"log/slog"
	"os"
	"reflect"

	"github.com/caarlos0/env/v10"
	secrets "github.com/harrisoncramer/ultra/secrets"
)

// Load parses T from the environment, returning an error if anything required is
// missing or malformed. Apps depend on this instead of the env library directly,
// so config parsing, and the underlying dependency, is controlled in one place.
//
// This is where secrets are actually read into the process, so it is also where
// a missing secret is reported: every field tagged `secret:"true"` whose env var
// is unset is warned about before parsing. Required ones then also fail the parse.
func Load[T any]() (T, error) {
	var cfg T

	for _, name := range secrets.SecretEnvNames(reflect.TypeFor[T]()) {
		if os.Getenv(name) == "" {
			slog.Warn("secret not present in environment", "name", name)
		}
	}

	if err := env.Parse(&cfg); err != nil {
		return cfg, fmt.Errorf("failed to load config: %w", err)
	}
	return cfg, nil
}
