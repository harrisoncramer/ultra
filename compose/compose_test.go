package ultra_test

import (
	_ "embed"
	"testing"

	"github.com/harrisoncramer/ultra"
)

//go:embed testdata/worker_override.golden
var workerOverrideGolden string

func TestComposeVar(t *testing.T) {
	cases := []struct {
		app, name, want string
	}{
		{"worker", "DATABASE_URL", "ULTRA_WORKER__DATABASE_URL"},
		{"server", "DATABASE_URL", "ULTRA_SERVER__DATABASE_URL"},
		{"dafpay-network", "API_KEY", "ULTRA_DAFPAY_NETWORK__API_KEY"},
	}
	for _, c := range cases {
		if got := ultra.ComposeVar(c.app, c.name); got != c.want {
			t.Errorf("ComposeVar(%q, %q) = %q, want %q", c.app, c.name, got, c.want)
		}
	}
}

func TestComposeVarNoCollisionAcrossApps(t *testing.T) {
	// The same secret name in two apps must map to distinct launcher variables.
	if a, b := ultra.ComposeVar("worker", "DATABASE_URL"), ultra.ComposeVar("server", "DATABASE_URL"); a == b {
		t.Fatalf("expected distinct vars for the same name in different apps, both were %q", a)
	}
}

func TestComposeOverride(t *testing.T) {
	got := ultra.ComposeOverride("worker", []string{"DATABASE_URL", "GOOGLE_CLIENT_ID"})
	if got != workerOverrideGolden {
		t.Errorf("ComposeOverride mismatch:\n got: %q\nwant: %q", got, workerOverrideGolden)
	}
}
