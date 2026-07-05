package secrets

import (
	"fmt"
	"go/types"
	"reflect"
	"strings"

	"golang.org/x/tools/go/packages"
)

// SecretNames type-checks the Go package at dir and returns the env-var names of
// every field tagged `secret:"true"` reachable from its exported Config struct.
// It follows embedded and nested struct fields wherever they're
// defined, including sub-structs in other packages. It fails if the package has
// no exported Config struct.
func SecretNames(dir string) ([]string, error) {
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

	var names []string
	seen := map[string]struct{}{}
	visited := map[*types.Struct]bool{}

	var visit func(s *types.Struct)
	visit = func(s *types.Struct) {
		if visited[s] {
			return
		}
		visited[s] = true
		for i := 0; i < s.NumFields(); i++ {
			// Recurse into struct-typed fields (embedded or named), mirroring how
			// env.Parse descends into them. Type info resolves them uniformly, so
			// a sub-struct from another package is followed just like a local one.
			if child := structUnder(s.Field(i).Type()); child != nil {
				visit(child)
			}
			tag := reflect.StructTag(s.Tag(i))
			if tag.Get("secret") != "true" {
				continue
			}
			name, _, _ := strings.Cut(tag.Get("env"), ",") // "NAME,required" -> "NAME"
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
	visit(st)

	return names, nil
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

// secretEnvNames reflects over t and returns the env-var names of every field
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
