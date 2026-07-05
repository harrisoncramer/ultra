// Package ultra wires the secrets an app declares in its typed config struct
// (via `secret:"true"` tags) through a swappable resolver into the environment.
package ultra

import (
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"

	"github.com/caarlos0/env/v10"
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

	for _, name := range secretEnvNames(reflect.TypeFor[T]()) {
		if os.Getenv(name) == "" {
			slog.Warn("secret not present in environment", "name", name)
		}
	}

	if err := env.Parse(&cfg); err != nil {
		return cfg, fmt.Errorf("failed to load config: %w", err)
	}
	return cfg, nil
}

// secretEnvNames reflects over t and returns the env-var names of every field
// tagged `secret:"true"`, following embedded and nested structs like env.Parse
// does.
func secretEnvNames(t reflect.Type) []string {
	var names []string
	seen := map[string]struct{}{}
	visited := map[reflect.Type]bool{}

	var visit func(t reflect.Type)
	visit = func(t reflect.Type) {
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || visited[t] {
			return
		}
		visited[t] = true
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			visit(f.Type)
			if f.Tag.Get("secret") != "true" {
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("env"), ",") // "NAME,required" -> "NAME"
			if name == "" {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	visit(t)

	return names
}
