package cli

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

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

func TestAWSResolve(t *testing.T) {
	cases := []struct {
		name           string
		app            string
		prefix         string
		store          map[string]string // secret id -> value the fake store holds
		request        []string          // names Resolve is asked for
		want           map[string]string // expected trailing-name -> value
		wantBatchSizes []int             // id count per BatchGetSecretValue call
	}{
		{
			name:   "maps trailing names, omits missing and empty",
			app:    "worker",
			prefix: "prod",
			store: map[string]string{
				"prod/worker/DATABASE_URL": "postgres://db",
				"prod/worker/EMPTY":        "",
			},
			request:        []string{"DATABASE_URL", "EMPTY", "ABSENT"},
			want:           map[string]string{"DATABASE_URL": "postgres://db"},
			wantBatchSizes: []int{3},
		},
		{
			name:           "resolves without a prefix",
			app:            "worker",
			prefix:         "",
			store:          map[string]string{"worker/API_KEY": "sekret"},
			request:        []string{"API_KEY"},
			want:           map[string]string{"API_KEY": "sekret"},
			wantBatchSizes: []int{1},
		},
		{
			name:   "batches ids in chunks of twenty",
			app:    "worker",
			prefix: "",
			store:  map[string]string{},
			request: []string{
				"N01", "N02", "N03", "N04", "N05", "N06", "N07", "N08", "N09", "N10",
				"N11", "N12", "N13", "N14", "N15", "N16", "N17", "N18", "N19", "N20",
				"N21",
			},
			want:           map[string]string{},
			wantBatchSizes: []int{20, 1},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fake := &fakeBatchGet{values: c.store}
			r := awsSecretsManager{
				app:    c.app,
				prefix: c.prefix,
				newAPI: func(context.Context) (batchGetAPI, error) { return fake, nil },
			}

			got, err := r.Resolve(context.Background(), c.request)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}

			if len(got) != len(c.want) {
				t.Fatalf("Resolve = %v, want %v", got, c.want)
			}
			for name, val := range c.want {
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
