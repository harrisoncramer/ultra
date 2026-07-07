package cli

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

// Tests for the AWS Secrets Manager resolver: how it names secrets
// (<prefix>/<app>/<NAME>), maps fetched values back to their env-var names,
// drops missing or empty ones, and splits requests into AWS's 20-per-call batch
// limit. A fake client stands in for the SDK, so nothing here reaches AWS.

func TestAWSSecretID(t *testing.T) {
	cases := []struct {
		app, prefix, name, want string
	}{
		{"worker", "", "GOOGLE_CLIENT_ID", "worker/GOOGLE_CLIENT_ID"},
		{"worker", "prod", "DATABASE_URL", "prod/worker/DATABASE_URL"},
		{"worker", "/prod/", "API_KEY", "prod/worker/API_KEY"},
	}
	for _, c := range cases {
		r := awsSecretsManager{app: c.app, prefix: c.prefix}
		if got := r.secretID(c.name); got != c.want {
			t.Errorf("secretID(%q) app=%q prefix=%q = %q, want %q", c.name, c.app, c.prefix, got, c.want)
		}
	}
}

// fakeBatchGet records every id list it is asked for and returns a fixed set of
// secret values, letting the resolver be tested without reaching AWS.
type fakeBatchGet struct {
	values map[string]string
	calls  [][]string
}

func (f *fakeBatchGet) BatchGetSecretValue(_ context.Context, in *secretsmanager.BatchGetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.BatchGetSecretValueOutput, error) {
	f.calls = append(f.calls, in.SecretIdList)
	var out secretsmanager.BatchGetSecretValueOutput
	for _, id := range in.SecretIdList {
		val, ok := f.values[id]
		if !ok {
			continue
		}
		out.SecretValues = append(out.SecretValues, smtypes.SecretValueEntry{
			Name:         aws.String(id),
			SecretString: aws.String(val),
		})
	}
	return &out, nil
}

// resolveCase is one Resolve scenario: what the fake store holds, the names the
// caller asks for, and the result and batch layout expected back.
type resolveCase struct {
	name           string
	app            string
	prefix         string
	storedByID     map[string]string // secret id (<prefix>/<app>/<NAME>) -> value held in the store
	requestNames   []string          // trailing names Resolve is asked for
	wantByName     map[string]string // expected trailing name -> value
	wantBatchSizes []int             // id count per BatchGetSecretValue call
}

func TestAWSResolve(t *testing.T) {
	cases := []resolveCase{
		{
			name:   "maps trailing names, omits missing and empty",
			app:    "worker",
			prefix: "prod",
			storedByID: map[string]string{
				"prod/worker/DATABASE_URL": "postgres://db",
				"prod/worker/EMPTY":        "",
			},
			requestNames:   []string{"DATABASE_URL", "EMPTY", "ABSENT"},
			wantByName:     map[string]string{"DATABASE_URL": "postgres://db"},
			wantBatchSizes: []int{3},
		},
		{
			name:           "resolves without a prefix",
			app:            "worker",
			prefix:         "",
			storedByID:     map[string]string{"worker/API_KEY": "sekret"},
			requestNames:   []string{"API_KEY"},
			wantByName:     map[string]string{"API_KEY": "sekret"},
			wantBatchSizes: []int{1},
		},
		{
			name:       "batches ids in chunks of twenty",
			app:        "worker",
			prefix:     "",
			storedByID: map[string]string{},
			requestNames: []string{
				"N01", "N02", "N03", "N04", "N05", "N06", "N07", "N08", "N09", "N10",
				"N11", "N12", "N13", "N14", "N15", "N16", "N17", "N18", "N19", "N20",
				"N21",
			},
			wantByName:     map[string]string{},
			wantBatchSizes: []int{20, 1},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &fakeBatchGet{values: c.storedByID}
			r := awsSecretsManager{
				app:    c.app,
				prefix: c.prefix,
				newAPI: func(context.Context) (batchGetAPI, error) { return fake, nil },
			}

			got, err := r.Resolve(context.Background(), c.requestNames)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}

			if len(got) != len(c.wantByName) {
				t.Fatalf("Resolve = %v, want %v", got, c.wantByName)
			}
			for name, val := range c.wantByName {
				if got[name] != val {
					t.Errorf("Resolve[%q] = %q, want %q", name, got[name], val)
				}
			}

			if len(fake.calls) != len(c.wantBatchSizes) {
				t.Fatalf("got %d batches, want %d", len(fake.calls), len(c.wantBatchSizes))
			}
			for i, want := range c.wantBatchSizes {
				if len(fake.calls[i]) != want {
					t.Errorf("batch %d size = %d, want %d", i, len(fake.calls[i]), want)
				}
			}
		})
	}
}
