// Package scan is the config-scanning domain: it reads an app's exported Config
// struct and reports the env-var fields it declares. It wraps pkg/secrets behind
// a Scanner service so the run, validate and lint domains depend on an injected
// interface rather than the package-level functions.
package scan

import (
	"fmt"
	"go/types"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/harrisoncramer/ultra/internal/xmap"
	"github.com/harrisoncramer/ultra/internal/xstring"
	"golang.org/x/tools/go/packages"
)

// Scanner reads config packages off disk and reports their declared fields. It
// caches the Config struct it type-checks per directory, so asking about the same
// app twice type-checks it once.
type Scanner struct {
	mu      sync.Mutex
	structs map[string]*types.Struct
}

// NewScanner returns a Scanner with an empty type-check cache.
func NewScanner() *Scanner { return &Scanner{structs: map[string]*types.Struct{}} }

// Prepare type-checks the given config packages in one packages.Load and caches
// what it resolves, so a later Fields call for any of them is served from memory.
// One load walks the dependency graph those packages share once instead of once
// per directory, and that graph is nearly all of the work when several apps embed
// the same shared config struct.
//
// It is advisory, never authoritative: a dir it cannot resolve is left uncached
// and Fields loads it on its own, reporting the real error there. That covers a
// package with a genuine problem as well as dirs that cannot be loaded together
// at all, as separate modules with no workspace tying them together cannot be.
func (s *Scanner) Prepare(dirs []string) {
	if len(dirs) < 2 {
		return
	}
	loaded := configStructs(dirs)
	s.mu.Lock()
	defer s.mu.Unlock()
	for dir, st := range loaded {
		s.structs[dir] = st
	}
}

// Fields returns every env-var field reachable from the exported Config struct in
// the package at dir.
func (s *Scanner) Fields(dir string) ([]Field, error) {
	s.mu.Lock()
	st, ok := s.structs[dir]
	s.mu.Unlock()
	if ok {
		return fieldsOf(dir, st)
	}
	st, err := configStruct(dir)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.structs[dir] = st
	s.mu.Unlock()
	return fieldsOf(dir, st)
}

// SecretNames returns the env-var names of every field tagged secret:"true" in
// the package at dir.
func (s *Scanner) SecretNames(dir string) ([]string, error) {
	fields, err := s.Fields(dir)
	if err != nil {
		return nil, err
	}
	return secretNamesOf(fields), nil
}

// ConfigImportPath returns the import path of the config package at dir, so a
// generated program can import it and call its Load.
func (s *Scanner) ConfigImportPath(dir string) (string, error) {
	return ConfigImportPath(dir)
}

// DeclaredNames is the set of every env-var name the given fields reference,
// secret and non-secret alike.
func DeclaredNames(fields []Field) map[string]struct{} {
	declared := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		declared[f.Name] = struct{}{}
	}
	return declared
}

// Unreferenced returns the keys in provided that no declared name covers. In other words, the
// values a Resolver supplies that no Config field reads.
func Unreferenced(provided map[string]string, declared map[string]struct{}) []string {
	var extra []string
	for k := range provided {
		if _, ok := declared[k]; !ok {
			extra = append(extra, k)
		}
	}
	return extra
}

// NewFakeConfigScanner returns a ConfigScanner that just returns the values provided (echoes them back).
func NewFakeConfigScanner(fields []Field) *FakeConfigScanner {
	return &FakeConfigScanner{
		fields: fields,
	}
}

// FakeConfigScanner is used to simulate the code that scans an app's Config fields.
type FakeConfigScanner struct {
	fields []Field
}

func (f FakeConfigScanner) Fields(string) ([]Field, error) {
	return f.fields, nil
}

func (f FakeConfigScanner) ConfigImportPath(string) (string, error) { return "example.com/x", nil }

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
	IsSecret     bool     // the field is tagged secret:"true"
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
	return fieldsOf(dir, st)
}

// fieldsOf reports the env-var fields of an already type-checked Config struct, so
// a struct loaded once can be scanned without loading it again. dir names the
// package in error messages and nothing else.
func fieldsOf(dir string, st *types.Struct) ([]Field, error) {
	var fields []Field
	var badEnvTag []string

	// A duplicate env name is always an error, reported after the scan, but with
	// two distinct messages: conflicting names are declared with differing
	// secret-ness (resolved from two sources at once), redeclared names with the
	// same secret-ness (a plain collision). declaredSecret records each name's
	// first-seen secret-ness and doubles as the seen set.
	conflicting := map[string]struct{}{}
	redeclared := map[string]struct{}{}
	declaredSecret := map[string]bool{}
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
				requiredEnvs = xstring.SplitBy(r, ",")
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
			if xstring.CommaSeparatedHasValue(opts, "required") || xstring.CommaSeparatedHasValue(opts, "notEmpty") {
				badEnvTag = append(badEnvTag, name)
				continue
			}
			secret := tag.Get("secret") == "true"
			if prevSecret, dup := declaredSecret[name]; dup {
				// The same env name can't come from two fields: caarlos0/env would set
				// both from one variable. If the two disagree on secret-ness the name
				// would be resolved from both the store and the config map at once; if
				// they agree it is a plain redeclaration. Either way, refuse rather
				// than silently pick a winner (or quietly join their required scopes),
				// which hides the mistake and is hard to track down later.
				if prevSecret != secret {
					conflicting[name] = struct{}{}
				} else if _, isConflict := conflicting[name]; !isConflict {
					redeclared[name] = struct{}{}
				}
				continue
			}
			declaredSecret[name] = secret
			fields = append(fields, Field{
				Name:         name,
				IsSecret:     secret,
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
		return nil, fmt.Errorf("config at %s: %s declared both as a secret and as non-secret config; a name is resolved from one source, so tag it secret:\"true\" in every field that declares it or in none", dir, strings.Join(xmap.SortedKeys(conflicting), ", "))
	}
	if len(redeclared) > 0 {
		return nil, fmt.Errorf("config at %s: %s declared by more than one field; each env name must be declared exactly once", dir, strings.Join(xmap.SortedKeys(redeclared), ", "))
	}
	return fields, nil
}

// SecretNames returns the env-var names of every field tagged `secret:"true"`
// reachable from the exported Config struct at dir.
func SecretNames(dir string) ([]string, error) {
	fields, err := Fields(dir)
	if err != nil {
		return nil, err
	}
	return secretNamesOf(fields), nil
}

// secretNamesOf reports the env-var names of the secret-tagged fields.
func secretNamesOf(fields []Field) []string {
	var names []string
	for _, f := range fields {
		if f.IsSecret {
			names = append(names, f.Name)
		}
	}
	return names
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

// configStructs type-checks the given packages in a single packages.Load and
// returns each Config struct it could resolve, keyed by the directory it was
// asked for. NeedFiles is in the mode so a loaded package can be mapped back to
// the directory it came from, since go/packages reports results in no particular
// order.
//
// A package it cannot resolve is omitted rather than failing the batch, so one
// bad app does not cost every other app the shared load. The result is a cache to
// consult, not the answer.
func configStructs(dirs []string) map[string]*types.Struct {
	patterns := make([]string, 0, len(dirs))
	byDir := make(map[string]string, len(dirs))
	for _, dir := range dirs {
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		byDir[abs] = dir
		patterns = append(patterns, "./"+filepath.ToSlash(dir))
	}

	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedTypes | packages.NeedImports,
	}, patterns...)
	if err != nil {
		return nil
	}

	out := make(map[string]*types.Struct, len(pkgs))
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 || len(pkg.GoFiles) == 0 || pkg.Types == nil {
			continue
		}
		dir, ok := byDir[filepath.Dir(pkg.GoFiles[0])]
		if !ok {
			continue
		}
		obj := pkg.Types.Scope().Lookup("Config")
		if obj == nil {
			continue
		}
		if st := structUnder(obj.Type()); st != nil {
			out[dir] = st
		}
	}
	return out
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
