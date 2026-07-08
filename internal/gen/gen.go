// Package gen is the generate domain: it writes each app's names-only docker
// compose override from the secret names its Config declares. Generation is a
// static operation over each app's config package — it never touches the secret
// store — so it works where the store is unreachable, and its output can be
// committed once and reused by run.
package gen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/harrisoncramer/ultra/internal/project"
)

// scanner reports the secret env-var names an app's Config declares.
type scanner interface {
	SecretNames(dir string) ([]string, error)
}

// composer renders the names-only compose override for an app.
type composer interface {
	Override(app string, names []string) string
}

// Generator writes apps' compose overrides from their declared secret names.
type Generator struct {
	scanner     scanner
	composer    composer
	project     project.Project
	overrideDir string
}

// NewGeneratorParams are the dependencies and layout NewGenerator needs.
type NewGeneratorParams struct {
	Scanner     scanner
	Composer    composer
	Project     project.Project
	OverrideDir string // dir under root the overrides are written to; empty means "tmp"
}

// NewGenerator builds a Generator, defaulting the override dir.
func NewGenerator(params NewGeneratorParams) *Generator {
	overrideDir := params.OverrideDir
	if overrideDir == "" {
		overrideDir = "tmp"
	}
	return &Generator{
		scanner:     params.Scanner,
		composer:    params.Composer,
		project:     params.Project,
		overrideDir: overrideDir,
	}
}

// AppOverride is one app's generated override: the secret names its Config
// declares and the override file written for it. Path is empty when the app
// declares no secrets, in which case no file is written.
type AppOverride struct {
	App   string
	Names []string
	Path  string
}

// Generate writes each app's compose override listing every secret name its
// Config declares, independent of any secret store, and returns the result per
// app in input order. An app that declares no secrets gets no file.
func (g *Generator) Generate(apps []string) ([]AppOverride, error) {
	out := make([]AppOverride, 0, len(apps))
	for _, appPath := range apps {
		app := g.project.AppName(appPath)
		names, err := g.scanner.SecretNames(g.project.AppConfigDir(appPath))
		if err != nil {
			return nil, fmt.Errorf("reading %s config: %w", app, err)
		}
		if len(names) == 0 {
			out = append(out, AppOverride{App: app})
			continue
		}
		sort.Strings(names)

		path := filepath.Join(g.project.Root, g.overrideDir, app+".compose.yml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("creating override dir for %s: %w", app, err)
		}
		if err := os.WriteFile(path, []byte(g.composer.Override(app, names)), 0o644); err != nil {
			return nil, fmt.Errorf("writing compose override for %s: %w", app, err)
		}
		out = append(out, AppOverride{App: app, Names: names, Path: path})
	}
	return out, nil
}
