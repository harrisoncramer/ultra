// Package gen is the generate domain: it writes a single names-only docker
// compose file covering every app, from the secret names each app's Config
// declares. Generation is a static operation over each app's config package; it
// never touches the secret store, so it works where the store is unreachable,
// and its output can be committed once and reused by run.
package gen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/harrisoncramer/ultra/internal/project"
	pkgcompose "github.com/harrisoncramer/ultra/pkg/compose"
)

// defaultOutputName is the combined compose file gen writes when no name is
// configured. It is deliberately not docker-compose.override.yml, whose fixed
// name compose auto-loads, so gen never clobbers a hand-written override.
const defaultOutputName = "ultra.compose.yml"

// scanner reports the secret env-var names an app's Config declares.
type scanner interface {
	SecretNames(dir string) ([]string, error)
}

// composer renders the single names-only compose file covering every app.
type composer interface {
	Override(apps []pkgcompose.AppSecrets) string
}

// Generator writes the combined compose file from apps' declared secret names.
type Generator struct {
	scanner        scanner
	composer       composer
	project        project.Project
	outputDir      string
	outputFilename string
}

// NewGeneratorParams are the dependencies and layout NewGenerator needs.
type NewGeneratorParams struct {
	Scanner        scanner
	Composer       composer
	Project        project.Project
	OutputDir      string // dir under root the file is written to; empty means "tmp"
	OutputFilename string // file name of the combined output; empty means the default
}

// NewGenerator builds a Generator, defaulting the output dir and file name.
func NewGenerator(params NewGeneratorParams) *Generator {
	outputDir := params.OutputDir
	if outputDir == "" {
		outputDir = "tmp"
	}
	outputFilename := params.OutputFilename
	if outputFilename == "" {
		outputFilename = defaultOutputName
	}
	return &Generator{
		scanner:        params.Scanner,
		composer:       params.Composer,
		project:        params.Project,
		outputDir:      outputDir,
		outputFilename: outputFilename,
	}
}

// AppOutput is one app's contribution to the combined file: the secret names its
// Config declares, in sorted order. An app that declares no secrets has no Names
// and contributes no service block.
type AppOutput struct {
	App   string
	Names []string
}

// Result is the outcome of a generation: each app's declared secret names in
// input order, and the single combined file written for them. Path is empty when
// no app declares any secret, in which case no file is written.
type Result struct {
	Apps []AppOutput
	Path string
}

// Generate writes a single compose file listing every secret name each app's
// Config declares, independent of any secret store, and returns each app's names
// in input order plus the written file's path. Apps that declare no secrets
// contribute no service block; if none declare any, no file is written.
func (g *Generator) Generate(apps []string) (Result, error) {
	out := make([]AppOutput, 0, len(apps))
	blocks := make([]pkgcompose.AppSecrets, 0, len(apps))
	seen := make(map[string]string, len(apps))
	for _, appPath := range apps {
		app := g.project.AppName(appPath)
		if prev, dup := seen[app]; dup {
			return Result{}, fmt.Errorf("app name %q is used by both %s and %s: their secrets share one namespace and would collide in the compose file", app, prev, appPath)
		}
		seen[app] = appPath
		names, err := g.scanner.SecretNames(g.project.AppConfigDir(appPath))
		if err != nil {
			return Result{}, fmt.Errorf("reading %s config: %w", app, err)
		}
		if len(names) == 0 {
			out = append(out, AppOutput{App: app})
			continue
		}
		sort.Strings(names)
		out = append(out, AppOutput{App: app, Names: names})
		blocks = append(blocks, pkgcompose.AppSecrets{App: app, Names: names})
	}

	if len(blocks) == 0 {
		return Result{Apps: out}, nil
	}

	path := filepath.Join(g.project.Root, g.outputDir, g.outputFilename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, fmt.Errorf("creating output dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(g.composer.Override(blocks)), 0o644); err != nil {
		return Result{}, fmt.Errorf("writing compose file: %w", err)
	}
	return Result{Apps: out, Path: path}, nil
}
