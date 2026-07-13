package aws

import (
	"context"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type secretIDCase struct {
	name       string
	app        string
	prefix     string
	secretName string
	want       string
}

func TestAWSSecretID(t *testing.T) {
	cases := []secretIDCase{
		{
			name:       "no prefix",
			app:        "worker",
			prefix:     "",
			secretName: "GOOGLE_CLIENT_ID",
			want:       "worker/GOOGLE_CLIENT_ID",
		},
		{
			name:       "with prefix",
			app:        "worker",
			prefix:     "prod",
			secretName: "DATABASE_URL",
			want:       "prod/worker/DATABASE_URL",
		},
		{
			name:       "slashes trimmed on prefix",
			app:        "worker",
			prefix:     "/prod/",
			secretName: "API_KEY",
			want:       "prod/worker/API_KEY",
		},
		{
			name:       "slashes trimmed on app",
			app:        "/worker/",
			prefix:     "prod",
			secretName: "API_KEY",
			want:       "prod/worker/API_KEY",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := awsSecretsManager{app: c.app, prefix: c.prefix}
			assert.Equal(t, c.want, r.secretID(c.secretName))
		})
	}
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
			fake := NewFakeBatchSecretGetter(NewFakeBatchSecretGetterParams{
				Values: c.storedByID,
			})
			r := awsSecretsManager{
				app:    c.app,
				prefix: c.prefix,
				newAPI: func(context.Context) (batchSecretGetter, error) {
					return fake, nil
				},
			}

			got, err := r.Resolve(context.Background(), c.requestNames)
			require.NoError(t, err)
			assert.Equal(t, c.wantByName, got)

			require.Len(t, fake.calls, len(c.wantBatchSizes))
			for i, want := range c.wantBatchSizes {
				assert.Len(t, fake.calls[i], want, "batch %d", i)
			}
		})
	}
}

type NewFakeBatchSecretGetterParams struct {
	Values map[string]string
	Calls  [][]string
}

// NewFakeBatchSecretGetter returns a batch secret getter that returns a list of predefined values, and records
// the id of each called for.
func NewFakeBatchSecretGetter(params NewFakeBatchSecretGetterParams) *fakeBatchSecretGetter {
	return &fakeBatchSecretGetter{
		values: params.Values,
		calls:  params.Calls,
	}
}

// fakeBatchSecretGetter records every id list it is asked for and returns a fixed set of
// secret values, letting the resolver be tested without reaching AWS.
type fakeBatchSecretGetter struct {
	values map[string]string
	calls  [][]string
}

var _ batchSecretGetter = (*fakeBatchSecretGetter)(nil)

func (f *fakeBatchSecretGetter) BatchGetSecretValue(_ context.Context, input *secretsmanager.BatchGetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.BatchGetSecretValueOutput, error) {
	f.calls = append(f.calls, input.SecretIdList)
	var out secretsmanager.BatchGetSecretValueOutput
	for _, id := range input.SecretIdList {
		val, ok := f.values[id]
		if !ok {
			continue
		}
		out.SecretValues = append(out.SecretValues, smtypes.SecretValueEntry{
			Name:         awssdk.String(id),
			SecretString: awssdk.String(val),
		})
	}
	return &out, nil
}
