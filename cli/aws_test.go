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

func TestAWSResolveMapsNamesAndOmitsMissing(t *testing.T) {
	fake := &fakeBatchGet{values: map[string]string{
		"prod/worker/DATABASE_URL": "postgres://db",
		"prod/worker/EMPTY":        "",
	}}
	r := awsSecretsManager{
		app:    "worker",
		prefix: "prod",
		newAPI: func(context.Context) (batchGetAPI, error) { return fake, nil },
	}

	got, err := r.Resolve(context.Background(), []string{"DATABASE_URL", "EMPTY", "ABSENT"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 1 || got["DATABASE_URL"] != "postgres://db" {
		t.Fatalf("Resolve = %v, want only DATABASE_URL mapped back to its trailing name", got)
	}
}

func TestAWSResolveBatchesInChunksOfTwenty(t *testing.T) {
	names := make([]string, 45)
	for i := range names {
		names[i] = "N" + string(rune('A'+i%26)) + string(rune('a'+i/26))
	}
	fake := &fakeBatchGet{values: map[string]string{}}
	r := awsSecretsManager{
		app:    "worker",
		newAPI: func(context.Context) (batchGetAPI, error) { return fake, nil },
	}

	if _, err := r.Resolve(context.Background(), names); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	wantSizes := []int{20, 20, 5}
	if len(fake.calls) != len(wantSizes) {
		t.Fatalf("got %d batches, want %d", len(fake.calls), len(wantSizes))
	}
	for i, want := range wantSizes {
		if len(fake.calls[i]) != want {
			t.Errorf("batch %d size = %d, want %d", i, len(fake.calls[i]), want)
		}
	}
}
