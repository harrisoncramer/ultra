package secrets

import (
	"fmt"
	"go/types"
	"reflect"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Field is one env-var field reachable from a Config struct.
type Field struct {
	Name     string   // env-var name (the part before the first comma of the env tag)
	Required bool     // the env tag carries `required` or `notEmpty` — the value must be set
	Secret   bool     // the field is tagged secret:"true"
	Scope    []string // environments the field applies to (from envScope, own or inherited); nil means every environment
}

// RequiredIn reports whether the field must be provided in environment. A scoped
// field is required exactly in the environments in its scope; an unscoped field
// is required in every environment when its env tag says so.
func (f Field) RequiredIn(environment string) bool {
	if len(f.Scope) > 0 {
		for _, s := range f.Scope {
			if s == environment {
				return true
			}
		}
		return false
	}
	return f.Required
}

// Fields type-checks the Go package at dir and returns every env-var field
// reachable from its exported Config struct, recording whether each is required
// and secret-tagged and the environment scope that applies to it. It follows
// embedded and nested struct fields wherever they're defined, including
// sub-structs in other packages, propagating a struct's envScope to its fields,
// and deduplicates by env-var name. It fails if the package has no exported
// Config struct.
func Fields(dir string) ([]Field, error) {
	st, err := configStruct(dir)
	if err != nil {
		return nil, err
	}

	var fields []Field
	seen := map[string]struct{}{}
	visited := map[*types.Struct]bool{}

	var visit func(s *types.Struct, inherited []string)
	visit = func(s *types.Struct, inherited []string) {
		if visited[s] {
			return
		}
		visited[s] = true
		for i := 0; i < s.NumFields(); i++ {
			tag := reflect.StructTag(s.Tag(i))
			// A field's scope is its own envScope tag, or the one inherited from
			// the struct it lives in. An embedded or nested struct passes its scope
			// down to its fields.
			scope := inherited
			if s2, ok := tag.Lookup("envScope"); ok {
				scope = splitScope(s2)
			}
			// Recurse into struct-typed fields (embedded or named), mirroring how
			// env.Parse descends into them. Type info resolves them uniformly, so
			// a sub-struct from another package is followed just like a local one.
			if child := structUnder(s.Field(i).Type()); child != nil {
				visit(child, scope)
			}
			name, opts, _ := strings.Cut(tag.Get("env"), ",") // "NAME,required" -> "NAME", "required"
			if name == "" {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			fields = append(fields, Field{
				Name:     name,
				Required: hasOption(opts, "required") || hasOption(opts, "notEmpty"),
				Secret:   tag.Get("secret") == "true",
				Scope:    scope,
			})
		}
	}
	visit(st, nil)

	return fields, nil
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
	if len(scope) == 0 {
		return nil
	}
	return scope
}

// SecretNames returns the env-var names of every field tagged `secret:"true"`
// reachable from the exported Config struct at dir.
func SecretNames(dir string) ([]string, error) {
	fields, err := Fields(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, f := range fields {
		if f.Secret {
			names = append(names, f.Name)
		}
	}
	return names, nil
}

// configStruct type-checks the Go package at dir and returns the underlying
// struct of its exported Config, failing if the package has no such struct.
func configStruct(dir string) (*types.Struct, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedImports | packages.NeedDeps,
		Dir:  dir,
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, fmt.Errorf("loading config package at %s: %w", dir, err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no Go package at %s", dir)
	}
	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		return nil, fmt.Errorf("config package at %s has errors: %v", dir, pkg.Errors[0])
	}

	obj := pkg.Types.Scope().Lookup("Config")
	if obj == nil {
		return nil, fmt.Errorf("package %s has no exported Config struct", dir)
	}
	st := structUnder(obj.Type())
	if st == nil {
		return nil, fmt.Errorf("config in %s is not a struct", dir)
	}
	return st, nil
}

// hasOption reports whether the comma-separated env-tag options contain want.
func hasOption(opts, want string) bool {
	for _, o := range strings.Split(opts, ",") {
		if strings.TrimSpace(o) == want {
			return true
		}
	}
	return false
}

// ConfigImportPath returns the import path of the Go package at dir, so a generated program can import it and call its Load.
func ConfigImportPath(dir string) (string, error) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName, Dir: dir}, ".")
	if err != nil {
		return "", fmt.Errorf("loading config package at %s: %w", dir, err)
	}
	if len(pkgs) == 0 || pkgs[0].PkgPath == "" {
		return "", fmt.Errorf("no Go package at %s", dir)
	}
	return pkgs[0].PkgPath, nil
}

// structUnder resolves t to the struct it ultimately is, dereferencing pointers
// and named types, or returns nil if t is not a struct.
func structUnder(t types.Type) *types.Struct {
	switch u := t.(type) {
	case *types.Pointer:
		return structUnder(u.Elem())
	case *types.Named:
		return structUnder(u.Underlying())
	case *types.Struct:
		return u
	default:
		return nil
	}
}

// SecretEnvNames reflects over t and returns the env-var names of every field
// tagged `secret:"true"`, following embedded and nested structs like env.Parse
// does.
func SecretEnvNames(t reflect.Type) []string {
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
			name, _, _ := strings.Cut(f.Tag.Get("env"), ",")
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
