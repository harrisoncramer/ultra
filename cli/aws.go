package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/pflag"
)

func init() {
	RegisterResolver(ResolverCommand{
		Name:  "aws-secret-manager",
		Short: "Resolve secrets from AWS Secrets Manager via the aws CLI",
		Long: "aws-secret-manager reads each app's secrets from AWS Secrets Manager, one\n" +
			"plaintext secret per value. A secret is named <app>/<NAME> by default — for\n" +
			"the app 'worker', the GOOGLE_CLIENT_ID secret is 'worker/GOOGLE_CLIENT_ID'.\n" +
			"Pass --prefix to add a leading segment, e.g. --prefix prod gives\n" +
			"'prod/worker/GOOGLE_CLIENT_ID'. All of an app's secrets are fetched in one\n" +
			"batch-get-secret-value call using your local AWS credentials.",
		Setup: func(fs *pflag.FlagSet) func(app string) Resolver {
			var region, prefix string
			fs.StringVar(&region, "region", "", "AWS region (defaults to the aws CLI's configured region)")
			fs.StringVar(&prefix, "prefix", "", "path segment prepended before the app, e.g. an environment name")
			return func(app string) Resolver {
				return awsSecretsManager{app: app, prefix: prefix, region: region}
			}
		},
	})
}

// awsSecretsManager resolves secrets from AWS Secrets Manager, one plaintext
// secret per value named <prefix>/<app>/<NAME> (the prefix is optional). All of
// an app's secrets are fetched in one batch-get-secret-value call via the aws
// CLI, which uses the local AWS credential chain, so no keys are passed here.
type awsSecretsManager struct {
	app    string
	prefix string
	region string
}

// awsBatchLimit is the maximum number of secret ids AWS accepts per
// batch-get-secret-value call.
const awsBatchLimit = 20

// Resolve fetches each app secret by id and maps it back to its trailing name. A
// missing secret lands in the response's Errors and is omitted; an auth or
// permission failure fails the call and is fatal.
func (a awsSecretsManager) Resolve(ctx context.Context, names []string) (map[string]string, error) {
	idToName := make(map[string]string, len(names))
	ids := make([]string, 0, len(names))
	for _, name := range names {
		id := a.secretID(name)
		idToName[id] = name
		ids = append(ids, id)
	}

	out := make(map[string]string, len(names))
	for start := 0; start < len(ids); start += awsBatchLimit {
		end := min(start+awsBatchLimit, len(ids))
		batch := ids[start:end]

		cmdArgs := []string{"secretsmanager", "batch-get-secret-value", "--output", "json"}
		if a.region != "" {
			cmdArgs = append(cmdArgs, "--region", a.region)
		}
		cmdArgs = append(cmdArgs, "--secret-id-list")
		cmdArgs = append(cmdArgs, batch...)

		cmd := exec.CommandContext(ctx, "aws", cmdArgs...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			return nil, fmt.Errorf("aws secretsmanager batch-get-secret-value: %s", msg)
		}

		var resp struct {
			SecretValues []struct {
				Name         string `json:"Name"`
				SecretString string `json:"SecretString"`
			} `json:"SecretValues"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
			return nil, fmt.Errorf("parsing aws secrets: %w", err)
		}

		for _, sv := range resp.SecretValues {
			if name, ok := idToName[sv.Name]; ok && sv.SecretString != "" {
				out[name] = sv.SecretString
			}
		}
	}
	return out, nil
}

// secretID is the Secrets Manager name for a secret: <prefix>/<app>/<NAME> (the
// prefix is optional).
func (a awsSecretsManager) secretID(name string) string {
	segs := make([]string, 0, 3)
	if a.prefix != "" {
		segs = append(segs, strings.Trim(a.prefix, "/"))
	}
	segs = append(segs, a.app, name)
	return strings.Join(segs, "/")
}
