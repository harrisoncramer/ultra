// Package compose is the compose-generation domain: it renders the names-only
// docker-compose override that forwards each app's resolved secrets. It wraps
// pkg/compose behind a Composer service so the run domain depends on an injected
// interface.
package compose

import pkgcompose "github.com/harrisoncramer/ultra/pkg/compose"

// Composer renders compose overrides and the namespaced launcher variable names.
type Composer struct{}

// NewComposer returns a Composer. It holds no state; the type exists so callers
// can depend on it as an injected interface.
func NewComposer() *Composer { return &Composer{} }

// Var returns the app-namespaced launcher variable an override maps a secret onto.
func (c *Composer) Var(app, name string) string {
	return pkgcompose.ComposeVar(app, name)
}

// Override renders the single compose override that maps every app's secrets
// onto their namespaced launcher variables. It contains references only, never
// values.
func (c *Composer) Override(apps []pkgcompose.AppSecrets) string {
	return pkgcompose.ComposeOverride(apps)
}
