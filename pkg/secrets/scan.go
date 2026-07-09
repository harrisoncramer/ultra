package secrets

import (
	"fmt"
	"go/types"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// visitKey identifies a struct reached at a given env-var prefix and inherited
// required scope. The scan's recursion guard keys on it so the same struct type
// reached under a different envPrefix or a different required scope is scanned
// again (its fields carry distinct names or required-ness), while a
// self-referential type reached identically still terminates.
type visitKey struct {
	s        *types.Struct
	prefix   string
	required string
}

// Field is one env-var field reachable from a Config struct.
type Field struct {
	Name         string   // env-var name the app reads: any accumulated envPrefix plus the part before the first comma of the env tag
	Secret       bool     // the field is tagged secret:"true"
	RequiredEnvs []string // environments the field is required in (from the required tag, own or inherited); "*" means all, nil means never
}

// RequiredIn reports whether the field must be provided in environment: true when
// it is required everywhere ("*") or names this environment.
func (f Field) RequiredIn(environment string) bool {
	for _, e := range f.RequiredEnvs {
		if e == "*" || e == environment {
			return true
		}
	}
	return false
}

// Fields type-checks the Go package at dir and returns every env-var field
// reachable from its exported Config struct, recording whether each is
// secret-tagged and the environments it is required in. It follows embedded and
// nested struct fields wherever they're defined, including sub-structs in other
// packages, propagating a struct's required tag to its fields, and deduplicates
// by env-var name. It fails if the package has no exported Config struct, or if
// a field declares required/notEmpty in its env tag; required-ness must use the
// required tag instead.
func Fields(dir string) ([]Field, error) {
	st, err := configStruct(dir)
	if err != nil {
		return nil, err
	}

	var fields []Field
	var badEnvTag []string
	// conflicting collects names declared both as a secret and as non-secret
	// config by different fields; that is a hard error, reported after the scan.
	conflicting := map[string]struct{}{}
	// seenIdx maps a final env name to its slot in fields, so a name declared by
	// more than one reachable field lands on one entry rather than the first
	// occurrence silently winning.
	seenIdx := map[string]int{}
	visited := map[visitKey]bool{}

	var visit func(s *types.Struct, prefix string, inherited []string)
	visit = func(s *types.Struct, prefix string, inherited []string) {
		// Key the recursion guard on the prefix and inherited required scope as
		// well as the struct: the same struct type reached under a different
		// envPrefix (e.g. DB_ and CACHE_) or a different required scope yields
		// distinct names or required-ness and must be scanned again, while a
		// self-referential type reached identically still terminates.
		key := visitKey{s, prefix, strings.Join(inherited, ",")}
		if visited[key] {
			return
		}
		visited[key] = true
		for i := 0; i < s.NumFields(); i++ {
			// env.Parse skips fields it can't set, so an unexported field is never
			// populated at runtime; treat it as not declared here too, rather than
			// forwarding a secret the app can never read.
			if !s.Field(i).Exported() {
				continue
			}
			tag := reflect.StructTag(s.Tag(i))
			// A field's required environments are its own required tag, or those
			// inherited from the struct it lives in. An embedded or nested struct
			// passes its required tag down to its fields.
			requiredEnvs := inherited
			if r, ok := tag.Lookup("required"); ok {
				requiredEnvs = splitEnvs(r)
			}
			// Recurse into struct-typed fields (embedded or named), mirroring how
			// env.Parse descends into them. A struct field's envPrefix stacks on
			// top of the prefix already accumulated, so nested env vars carry the
			// full prefix env.Parse reads them under. Type info resolves sub-structs
			// uniformly, so one from another package is followed just like a local one.
			if child := structUnder(s.Field(i).Type()); child != nil {
				visit(child, prefix+tag.Get("envPrefix"), requiredEnvs)
			}
			name, opts, _ := strings.Cut(tag.Get("env"), ",")
			if name == "" {
				continue
			}
			// The launcher variable and the app's env.Parse must agree on the name,
			// so record the prefixed name, the one the app actually reads.
			name = prefix + name
			if hasOption(opts, "required") || hasOption(opts, "notEmpty") {
				badEnvTag = append(badEnvTag, name)
				continue
			}
			secret := tag.Get("secret") == "true"
			if idx, dup := seenIdx[name]; dup {
				// The same env name is resolved from exactly one source: the secret
				// store if secret, the config map otherwise. A name declared both ways
				// by different fields is contradictory, so fail rather than guess.
				if fields[idx].Secret != secret {
					conflicting[name] = struct{}{}
					continue
				}
				// Same-source duplicates are fine; caarlos0/env populates every field
				// carrying the name from the one variable, so it is required in the
				// union of the scopes any of them require.
				fields[idx].RequiredEnvs = unionEnvs(fields[idx].RequiredEnvs, requiredEnvs)
				continue
			}
			seenIdx[name] = len(fields)
			fields = append(fields, Field{
				Name:         name,
				Secret:       secret,
				RequiredEnvs: requiredEnvs,
			})
		}
	}
	visit(st, "", nil)

	if len(badEnvTag) > 0 {
		sort.Strings(badEnvTag)
		return nil, fmt.Errorf("config at %s: %s declare required/notEmpty in the env tag; declare required-ness with the required tag instead", dir, strings.Join(badEnvTag, ", "))
	}
	if len(conflicting) > 0 {
		names := make([]string, 0, len(conflicting))
		for name := range conflicting {
			names = append(names, name)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("config at %s: %s declared both as a secret and as non-secret config; a name is resolved from one source, so tag it secret:\"true\" in every field that declares it or in none", dir, strings.Join(names, ", "))
	}
	return fields, nil
}

// unionEnvs merges two required-scope lists: a field required in the scopes of
// any path that declares it. "*" (required everywhere) absorbs the rest, and two
// empty lists stay nil (never required). The result is sorted for determinism.
func unionEnvs(a, b []string) []string {
	set := make(map[string]struct{}, len(a)+len(b))
	for _, e := range a {
		set[e] = struct{}{}
	}
	for _, e := range b {
		set[e] = struct{}{}
	}
	if _, all := set["*"]; all {
		return []string{"*"}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for e := range set {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
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
	// NeedTypes type-checks the package at dir; NeedImports resolves its imports
	// from export data so an embedded or nested struct defined in another package
	// still resolves. NeedDeps is deliberately omitted: it type-checks the whole
	// transitive dependency closure from source and holds it in memory, which for
	// an app config that embeds a shared struct (pulling in large SDKs) costs
	// gigabytes and is slow. Reading only field tags needs none of that.
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedImports,
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
		return nil, fmt.Errorf("config package at %s has errors: %w", dir, pkg.Errors[0])
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
	type key struct {
		t      reflect.Type
		prefix string
	}
	visited := map[key]bool{}

	var visit func(t reflect.Type, prefix string)
	visit = func(t reflect.Type, prefix string) {
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || visited[key{t, prefix}] {
			return
		}
		visited[key{t, prefix}] = true
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			// A struct field's envPrefix stacks on the accumulated prefix, so nested
			// secrets carry the full name env.Parse reads them under.
			visit(f.Type, prefix+f.Tag.Get("envPrefix"))
			if f.Tag.Get("secret") != "true" {
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("env"), ",")
			if name == "" {
				continue
			}
			name = prefix + name
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	visit(t, "")

	return names
}
