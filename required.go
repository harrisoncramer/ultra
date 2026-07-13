package ultra

import (
	"reflect"
	"sort"
	"strings"

	"github.com/harrisoncramer/ultra/internal/xstring"
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

// walkAndCall walks the config value v and invokes fn for every leaf env-var
// field, with the environments it is required in (its own required tag, or the
// one inherited from the struct it is nested in) and the options of its env tag.
func walkAndCall(v reflect.Value, inherited []string, seen map[reflect.Type]bool, fn func(name, envOpts string, requiredEnvs []string, val reflect.Value)) {
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
			requiredEnvs = xstring.SplitBy(r, ",")
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct {
			walkAndCall(v.Field(i), requiredEnvs, seen, fn)
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

// hasDisallowedRequiredOrNotEmpty returns the names of fields that declare required or
// notEmpty inside their env tag. Required-ness must be declared with the required
// tag from ultra's library, rather than the caarlos0/env library's notEmpty helper,
// which ultra specifically  disallows in order to provide a uniform required syntax across environments.
func hasDisallowedRequiredOrNotEmpty(cfg any) []string {
	var out []string
	walkAndCall(reflect.ValueOf(cfg), nil, map[reflect.Type]bool{}, func(name, envOpts string, _ []string, _ reflect.Value) {
		for o := range strings.SplitSeq(envOpts, ",") {
			if strings.TrimSpace(o) == "required" || strings.TrimSpace(o) == "notEmpty" {
				out = append(out, name)
				continue
			}
		}
	})
	sort.Strings(out)
	return out
}

// requiredButMissingValue returns the env-var names of fields required in environment
// whose parsed value is empty. A field is required in the environments named by
// its required tag ("*" meaning all); outside them it is ignored.
func requiredButMissingValue(cfg any, environment string) []string {
	var out []string
	walkAndCall(reflect.ValueOf(cfg), nil, map[reflect.Type]bool{}, func(name, _ string, requiredEnvs []string, val reflect.Value) {

		isRequiredForEnvironment := false
		for _, e := range requiredEnvs {
			if e == "*" || e == environment {
				isRequiredForEnvironment = true
				break
			}
		}

		if isRequiredForEnvironment && val.IsZero() {
			out = append(out, name)
		}
	})
	sort.Strings(out)
	return out
}
