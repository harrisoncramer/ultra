package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path"
	"strings"

	"github.com/spf13/pflag"
)

func init() {
	RegisterResolver(ResolverCommand{
		Name:  "aws",
		Short: "Resolve secrets from AWS SSM Parameter Store via the aws CLI",
		Setup: func(fs *pflag.FlagSet) func(app string) Resolver {
			var region, prefix string
			fs.StringVar(&region, "region", "", "AWS region (defaults to the aws CLI's configured region)")
			fs.StringVar(&prefix, "prefix", "", "path segment prepended before the app, e.g. an environment name")
			return func(app string) Resolver {
				return awsParameterStore{app: app, prefix: prefix, region: region}
			}
		},
	})
}

// awsParameterStore resolves secrets from AWS SSM Parameter Store. Each secret is
// a parameter named /<prefix>/<app>/<NAME>; the whole path is read in one
// get-parameters-by-path call via the aws CLI, which uses the local AWS
// credential chain, so no keys are passed here.
type awsParameterStore struct {
	app    string
	prefix string
	region string
}

// Resolve reads every parameter under the app's path and picks out the requested
// names by their trailing segment. An auth or permission failure is fatal; a
// missing individual parameter is simply omitted from the result.
func (a awsParameterStore) Resolve(ctx context.Context, names []string) (map[string]string, error) {
	cmdArgs := []string{"ssm", "get-parameters-by-path", "--path", a.path(), "--recursive", "--with-decryption", "--output", "json"}
	if a.region != "" {
		cmdArgs = append(cmdArgs, "--region", a.region)
	}

	cmd := exec.CommandContext(ctx, "aws", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("aws ssm get-parameters-by-path %q: %s", a.path(), msg)
	}

	var resp struct {
		Parameters []struct {
			Name  string `json:"Name"`
			Value string `json:"Value"`
		} `json:"Parameters"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("parsing ssm parameters under %q: %w", a.path(), err)
	}

	want := make(map[string]struct{}, len(names))
	for _, name := range names {
		want[name] = struct{}{}
	}

	out := make(map[string]string, len(names))
	for _, p := range resp.Parameters {
		name := path.Base(p.Name)
		if _, ok := want[name]; ok && p.Value != "" {
			out[name] = p.Value
		}
	}
	return out, nil
}

// path is the SSM path the app's parameters live under: /<prefix>/<app> (the
// prefix is optional).
func (a awsParameterStore) path() string {
	segs := make([]string, 0, 2)
	if a.prefix != "" {
		segs = append(segs, strings.Trim(a.prefix, "/"))
	}
	segs = append(segs, a.app)
	return "/" + strings.Join(segs, "/")
}
