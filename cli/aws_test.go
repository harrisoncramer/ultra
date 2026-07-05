package cli

import "testing"

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
