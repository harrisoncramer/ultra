package ultra

import (
	"reflect"
	"sort"
	"strings"
)

// envScopeTag names the struct tag that scopes a field to specific environments.
const envScopeTag = "envScope"

// Option configures Load.
type Option func(*loadOptions)

type loadOptions struct {
	environment string
}

func newLoadOptions(opts []Option) loadOptions {
	var o loadOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithEnvironment declares the environment cfg is being loaded for. A field
// tagged `envScope:"a,b"` is required only when environment is a or b; without
// this option no environment matches, so scoped fields are treated as optional.
func WithEnvironment(environment string) Option {
	return func(o *loadOptions) { o.environment = environment }
}

// splitScope parses a comma-separated envScope tag into its environment names.
func splitScope(s string) []string {
	raw := strings.Split(s, ",")
	scope := make([]string, 0, len(raw))
	for _, p := range raw {
		if p = strings.TrimSpace(p); p != "" {
			scope = append(scope, p)
		}
	}
	return scope
}

func inScope(scope []string, environment string) bool {
	for _, s := range scope {
		if s == environment {
			return true
		}
	}
	return false
}

// hasEnvOption reports whether the comma-separated env-tag options contain want.
func hasEnvOption(opts, want string) bool {
	for _, o := range strings.Split(opts, ",") {
		if strings.TrimSpace(o) == want {
			return true
		}
	}
	return false
}

// eachEnvField walks the config value v and invokes fn for every leaf env-var
// field, with the environment scope that applies to it — its own envScope tag,
// or the scope inherited from the struct it is nested in. It mirrors how
// env.Parse descends into embedded and nested structs.
func eachEnvField(v reflect.Value, inherited []string, seen map[reflect.Type]bool, fn func(name, opts string, scope []string, val reflect.Value)) {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}
	t := v.Type()
	if seen[t] {
		return
	}
	seen[t] = true

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		scope := inherited
		if s, ok := f.Tag.Lookup(envScopeTag); ok {
			scope = splitScope(s)
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			eachEnvField(v.Field(i), scope, seen, fn)
			continue
		}
		if envTag, ok := f.Tag.Lookup("env"); ok {
			name, opts, _ := strings.Cut(envTag, ",")
			if name != "" {
				fn(name, opts, scope, v.Field(i))
			}
		}
	}
}

// scopeConflicts returns the env-var names of fields that carry both envScope and
// a required/notEmpty option in their env tag. That combination is contradictory:
// required is unconditional, so env.Parse would demand the field in every
// environment, defeating the scope. envScope alone already makes a field required
// within its environments.
func scopeConflicts(cfg any) []string {
	var out []string
	eachEnvField(reflect.ValueOf(cfg), nil, map[reflect.Type]bool{}, func(name, opts string, scope []string, _ reflect.Value) {
		if len(scope) > 0 && (hasEnvOption(opts, "required") || hasEnvOption(opts, "notEmpty")) {
			out = append(out, name)
		}
	})
	sort.Strings(out)
	return out
}

// missingScopedRequired returns the env-var names of scoped fields that apply to
// environment but whose parsed value is empty. A scoped field is required within
// the environments in its scope; outside them it is ignored.
func missingScopedRequired(cfg any, environment string) []string {
	var out []string
	eachEnvField(reflect.ValueOf(cfg), nil, map[reflect.Type]bool{}, func(name, opts string, scope []string, val reflect.Value) {
		if len(scope) == 0 || !inScope(scope, environment) {
			return
		}
		if val.IsZero() {
			out = append(out, name)
		}
	})
	sort.Strings(out)
	return out
}
