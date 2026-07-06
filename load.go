package ultra

import (
	"fmt"

	"github.com/caarlos0/env/v10"
)

// Load parses T from the environment, returning an error if anything required is
// missing or malformed. Apps depend on this instead of the env library directly,
// so config parsing, and the underlying dependency, is controlled in one place.
// Fields tagged `secret:"true"` are read here like any other; required ones fail
// the parse when unset.
func Load[T any]() (T, error) {
	var cfg T
	if err := env.Parse(&cfg); err != nil {
		return cfg, fmt.Errorf("failed to load config: %w", err)
	}
	return cfg, nil
}
