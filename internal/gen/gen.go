// Package gen is the generate domain: it writes a single names-only docker
// compose file covering every app, from the secret names each app's Config
// declares (read through the shared configreader). Generation is a static
// operation over each app's config package; it never touches the secret store,
// so it works where the store is unreachable, and its output can be committed
// once and reused by run.
package gen

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/harrisoncramer/ultra/internal/compose"
	"github.com/harrisoncramer/ultra/internal/configreader"
	"github.com/harrisoncramer/ultra/internal/project"
)

// DefaultOutputName is the combined compose file gen writes when no name is
// configured. It is deliberately not docker-compose.override.yml, whose fixed
// name compose auto-loads, so gen never clobbers a hand-written override.
const DefaultOutputName = "ultra.compose.yml"

// configReader reports the secret names each app's Config declares. It is the
// shared source of truth gen and run both read, so their view of the declared
// secrets never diverges.
type configReader interface {
	Read(apps []string) ([]configreader.AppOutput, error)
}

// composer renders the single combined compose override from the per-app secret
// name blocks.
type composer interface {
	Override(apps []compose.AppSecrets) string
}

// Generator writes the combined compose file from apps' declared secret names.
type Generator struct {
	reader         configReader
	composer       composer
	project        project.Project
	outputDir      string
	outputFilename string
}

// NewGeneratorParams are the dependencies and layout NewGenerator needs.
type NewGeneratorParams struct {
	Reader         configReader
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
		outputFilename = DefaultOutputName
	}
	return &Generator{
		reader:         params.Reader,
		composer:       params.Composer,
		project:        params.Project,
		outputDir:      outputDir,
		outputFilename: outputFilename,
	}
}

// OverridePath is the path gen writes the combined compose file to, computed
// from root, output dir and file name. run reads the same layout to locate the
// committed file without depending on gen's writer.
func OverridePath(root, outputDir, outputFilename string) string {
	if outputDir == "" {
		outputDir = "tmp"
	}
	if outputFilename == "" {
		outputFilename = DefaultOutputName
	}
	return filepath.Join(root, outputDir, outputFilename)
}

// Result is the outcome of a generation: each app's declared secret names in
// input order, and the single combined file written for them. Path is empty when
// no app declares any secret, in which case no file is written.
type Result struct {
	Apps []configreader.AppOutput
	Path string
}

// Generate writes a single compose file listing every secret name each app's
// Config declares, read through the shared configreader, and returns each app's
// names in input order plus the written file's path. Apps that declare no
// secrets contribute no service block; if none declare any, no file is written.
func (g *Generator) Generate(apps []string) (Result, error) {
	out, err := g.reader.Read(apps)
	if err != nil {
		return Result{}, err
	}

	blocks := make([]compose.AppSecrets, 0, len(out))
	for _, o := range out {
		if len(o.Names) == 0 {
			continue
		}
		blocks = append(blocks, compose.AppSecrets{App: o.App, Names: o.Names})
	}

	if len(blocks) == 0 {
		return Result{Apps: out}, nil
	}

	path := OverridePath(g.project.Root, g.outputDir, g.outputFilename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, fmt.Errorf("creating output dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(g.composer.Override(blocks)), 0o644); err != nil {
		return Result{}, fmt.Errorf("writing compose file: %w", err)
	}
	return Result{Apps: out, Path: path}, nil
}
