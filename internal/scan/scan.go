// Package scan is the config-scanning domain: it reads an app's exported Config
// struct and reports the env-var fields it declares. It wraps pkg/secrets behind
// a Scanner service so the run, validate and lint domains depend on an injected
// interface rather than the package-level functions.
package scan

import "github.com/harrisoncramer/ultra/pkg/secrets"

// Field is one env-var field reachable from a Config struct.
type Field = secrets.Field

// Scanner reads config packages off disk and reports their declared fields.
type Scanner struct{}

// NewScanner returns a Scanner. It holds no state; the type exists so callers can
// depend on it as an injected interface.
func NewScanner() *Scanner { return &Scanner{} }

// Fields returns every env-var field reachable from the exported Config struct in
// the package at dir.
func (s *Scanner) Fields(dir string) ([]Field, error) {
	return secrets.Fields(dir)
}

// SecretNames returns the env-var names of every field tagged secret:"true" in
// the package at dir.
func (s *Scanner) SecretNames(dir string) ([]string, error) {
	return secrets.SecretNames(dir)
}

// ConfigImportPath returns the import path of the config package at dir, so a
// generated program can import it and call its Load.
func (s *Scanner) ConfigImportPath(dir string) (string, error) {
	return secrets.ConfigImportPath(dir)
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

// Unreferenced returns the keys in provided that no declared name covers — the
// values a resolver supplies that no Config field reads.
func Unreferenced(provided map[string]string, declared map[string]struct{}) []string {
	var extra []string
	for k := range provided {
		if _, ok := declared[k]; !ok {
			extra = append(extra, k)
		}
	}
	return extra
}
