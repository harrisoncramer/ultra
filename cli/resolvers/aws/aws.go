package aws

import (
	"context"
	"fmt"
	"strings"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"

	"github.com/harrisoncramer/ultra/cli"

	"github.com/spf13/pflag"
)

func init() {
	cli.RegisterSecretResolver(cli.SecretResolverCommand{
		Name:  "aws-secret-manager",
		Short: "Resolve secrets from AWS Secrets Manager via the AWS SDK",
		Long: "aws-secret-manager reads each app's secrets from AWS Secrets Manager, one\n" +
			"plaintext secret per value. A secret is named <app>/<NAME> by default; for\n" +
			"the app 'worker', the GOOGLE_CLIENT_ID secret is 'worker/GOOGLE_CLIENT_ID'.\n" +
			"Pass --prefix to add a leading segment, e.g. --prefix prod gives\n" +
			"'prod/worker/GOOGLE_CLIENT_ID'. All of an app's secrets are fetched in one\n" +
			"BatchGetSecretValue call (up to 20 per call).\n\n" +
			"Credentials and the target account are resolved by the AWS SDK's default\n" +
			"credential chain: environment variables, --profile, ~/.aws SSO, or an IAM\n" +
			"role, so the account is whichever those credentials belong to. A profile\n" +
			"that sets role_arn/source_profile is honoured, so per-app or per-environment\n" +
			"roles work by pointing --profile at the right profile. Pass --profile to pin\n" +
			"a named profile instead of relying on the default; --region sets the region\n" +
			"when it isn't already configured for that profile.",
		Setup: func(fs *pflag.FlagSet) func(app string) cli.SecretResolver {
			var region, prefix, profile string
			fs.StringVar(&region, "region", "", "AWS region (defaults to the SDK's configured region)")
			fs.StringVar(&prefix, "prefix", "", "path segment prepended before the app, e.g. an environment name")
			fs.StringVar(&profile, "profile", "", "named AWS profile to use (defaults to the SDK's credential chain)")
			return func(app string) cli.SecretResolver {
				return awsSecretsManager{app: app, prefix: prefix, region: region, profile: profile}
			}
		},
	})
}

// awsBatchLimit is the maximum number of secret ids AWS accepts per
// BatchGetSecretValue call.
const awsBatchLimit = 20

// batchSecretGetter is the subset of the Secrets Manager client ultra uses; the
// resolver depends on it so tests can substitute a fake for the real SDK client.
type batchSecretGetter interface {
	BatchGetSecretValue(ctx context.Context, in *secretsmanager.BatchGetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.BatchGetSecretValueOutput, error)
}

// awsSecretsManager resolves secrets from AWS Secrets Manager, one plaintext
// secret per value named <prefix>/<app>/<NAME> (the prefix is optional). All of
// an app's secrets are fetched in one BatchGetSecretValue call via the AWS SDK,
// which uses the local AWS credential chain, so no keys are passed here.
type awsSecretsManager struct {
	app     string
	prefix  string
	region  string
	profile string
	// newAPI builds the Secrets Manager client; overridable in tests. A nil value
	// means load the real SDK client from the ambient credential chain.
	newAPI func(ctx context.Context) (batchSecretGetter, error)
}

// client returns the Secrets Manager client to resolve against, building the
// real SDK client from the region/profile flags unless a test has overridden it.
func (a awsSecretsManager) client(ctx context.Context) (batchSecretGetter, error) {
	if a.newAPI != nil {
		return a.newAPI(ctx)
	}
	var opts []func(*config.LoadOptions) error
	if a.region != "" {
		opts = append(opts, config.WithRegion(a.region))
	}
	if a.profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(a.profile))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading aws config: %w", err)
	}
	return secretsmanager.NewFromConfig(cfg), nil
}

// Resolve fetches each app secret by id and maps it back to its trailing name. A
// missing secret lands in the response's Errors and is omitted; an auth or
// permission failure fails the call and is fatal.
func (a awsSecretsManager) Resolve(ctx context.Context, names []string) (map[string]string, error) {
	api, err := a.client(ctx)
	if err != nil {
		return nil, err
	}

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
		resp, err := api.BatchGetSecretValue(ctx, &secretsmanager.BatchGetSecretValueInput{
			SecretIdList: ids[start:end],
		})
		if err != nil {
			return nil, fmt.Errorf("aws secretsmanager batch-get-secret-value: %w", err)
		}
		for _, sv := range resp.SecretValues {
			if name, ok := idToName[awssdk.ToString(sv.Name)]; ok && awssdk.ToString(sv.SecretString) != "" {
				out[name] = awssdk.ToString(sv.SecretString)
			}
		}
	}
	return out, nil
}

// secretID constructs the AWS Secrets Manager name for a secret: <prefix>/<app>/<NAME> (the prefix is optional).
func (a awsSecretsManager) secretID(name string) string {
	segments := make([]string, 0, 3)

	// Trim leading and trailing slashes if provided
	name = strings.Trim(name, "/")
	app := strings.Trim(a.app, "/")
	prefix := strings.Trim(a.prefix, "/")
	if prefix != "" {
		segments = append(segments, prefix)
	}
	segments = append(segments, app, name) // Then add app + secret name, and join
	return strings.Join(segments, "/")
}
