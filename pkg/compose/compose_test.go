package ultra_test

import (
	"os"
	"path/filepath"
	"testing"

	compose "github.com/harrisoncramer/ultra/pkg/compose"
)

func TestComposeVar(t *testing.T) {
	cases := []struct {
		app, name, want string
	}{
		{"worker", "DATABASE_URL", "ULTRA_WORKER__DATABASE_URL"},
		{"server", "DATABASE_URL", "ULTRA_SERVER__DATABASE_URL"},
		{"dafpay-network", "API_KEY", "ULTRA_DAFPAY_NETWORK__API_KEY"},
	}
	for _, c := range cases {
		if got := compose.ComposeVar(c.app, c.name); got != c.want {
			t.Errorf("ComposeVar(%q, %q) = %q, want %q", c.app, c.name, got, c.want)
		}
	}
}

func TestComposeVarNoCollisionAcrossApps(t *testing.T) {
	// The same secret name in two apps must map to distinct launcher variables.
	if a, b := compose.ComposeVar("worker", "DATABASE_URL"), compose.ComposeVar("server", "DATABASE_URL"); a == b {
		t.Fatalf("expected distinct vars for the same name in different apps, both were %q", a)
	}
}

func TestComposeOverride(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "testdata", "worker_override.golden"))
	if err != nil {
		t.Fatal(err)
	}
	got := compose.ComposeOverride("worker", []string{"DATABASE_URL", "GOOGLE_CLIENT_ID"})
	if got != string(want) {
		t.Errorf("ComposeOverride mismatch:\n got: %q\nwant: %q", got, string(want))
	}
}
