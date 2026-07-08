package ultra

import (
	"reflect"
	"sort"
	"strings"
)

// requiredTag names the struct tag that declares which environments a field is
// required in: "*" for every environment, a comma-separated list for specific
// ones, or absent for never required. A required field must be set and non-empty
// in an environment it applies to. Required-ness lives only in this tag; the env
// tag's own required/notEmpty options are not used (Load rejects them), so there
// is a single source of truth.
const requiredTag = "required"

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
// tagged `required:"a,b"` is enforced only when environment is a or b; a field
// tagged `required:"*"` is enforced in every environment, including when no
// environment is given.
func WithEnvironment(environment string) Option {
	return func(o *loadOptions) { o.environment = environment }
}

// splitEnvs parses a comma-separated required tag into its environment names.
func splitEnvs(s string) []string {
	raw := strings.Split(s, ",")
	envs := make([]string, 0, len(raw))
	for _, p := range raw {
		if p = strings.TrimSpace(p); p != "" {
			envs = append(envs, p)
		}
	}
	if len(envs) == 0 {
		return nil
	}
	return envs
}

// requiredIn reports whether a field required in requiredEnvs must be present in
// environment: true when the field is required everywhere ("*") or names this
// environment.
func requiredIn(requiredEnvs []string, environment string) bool {
	for _, e := range requiredEnvs {
		if e == "*" || e == environment {
			return true
		}
	}
	return false
}

// hasEnvOption reports whether the comma-separated env-tag options contain want.
func hasEnvOption(opts, want string) bool {
	for o := range strings.SplitSeq(opts, ",") {
		if strings.TrimSpace(o) == want {
			return true
		}
	}
	return false
}

// eachEnvField walks the config value v and invokes fn for every leaf env-var
// field, with the environments it is required in (its own required tag, or the
// one inherited from the struct it is nested in) and the options of its env tag.
// It mirrors how env.Parse descends into embedded and nested structs.
func eachEnvField(v reflect.Value, inherited []string, seen map[reflect.Type]bool, fn func(name, envOpts string, requiredEnvs []string, val reflect.Value)) {
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
		requiredEnvs := inherited
		if r, ok := f.Tag.Lookup(requiredTag); ok {
			requiredEnvs = splitEnvs(r)
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			eachEnvField(v.Field(i), requiredEnvs, seen, fn)
			continue
		}
		if envTag, ok := f.Tag.Lookup("env"); ok {
			name, opts, _ := strings.Cut(envTag, ",")
			if name != "" {
				fn(name, opts, requiredEnvs, v.Field(i))
			}
		}
	}
}

// envTagRequiredFields returns the names of fields that declare required or
// notEmpty inside their env tag. Required-ness must be declared with the required
// tag instead, so that it can be environment-aware and live in one place; env's
// own required is unconditional and would enforce the field in every environment
// behind ultra's back.
func envTagRequiredFields(cfg any) []string {
	var out []string
	eachEnvField(reflect.ValueOf(cfg), nil, map[reflect.Type]bool{}, func(name, envOpts string, _ []string, _ reflect.Value) {
		if hasEnvOption(envOpts, "required") || hasEnvOption(envOpts, "notEmpty") {
			out = append(out, name)
		}
	})
	sort.Strings(out)
	return out
}

// missingRequired returns the env-var names of fields required in environment
// whose parsed value is empty. A field is required in the environments named by
// its required tag ("*" meaning all); outside them it is ignored.
func missingRequired(cfg any, environment string) []string {
	var out []string
	eachEnvField(reflect.ValueOf(cfg), nil, map[reflect.Type]bool{}, func(name, _ string, requiredEnvs []string, val reflect.Value) {
		if requiredIn(requiredEnvs, environment) && val.IsZero() {
			out = append(out, name)
		}
	})
	sort.Strings(out)
	return out
}
