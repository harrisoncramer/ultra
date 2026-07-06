package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func loadFrom(t *testing.T, body string) fileConfig {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, configFileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	v := viper.New()
	v.SetConfigFile(filepath.Join(dir, configFileName))
	v.SetConfigType("toml")
	if err := v.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}
	return flatten(v)
}

func TestFlattenSections(t *testing.T) {
	fc := loadFrom(t, `
apps-dir = "services"

[secrets]
resolver = "aws-secret-manager"

[secrets.aws-secret-manager]
region = "us-east-1"
profile = "prod"

[config]
resolver = "docker-compose"
`)
	cases := map[string]string{
		"secret-resolver": "aws-secret-manager",
		"region":          "us-east-1",
		"profile":         "prod",
		"config-resolver": "docker-compose",
	}
	for flag, want := range cases {
		if got := fc.flags[flag]; got != want {
			t.Errorf("fc.flags[%q] = %q, want %q", flag, got, want)
		}
	}
}

func TestFlattenApps(t *testing.T) {
	fc := loadFrom(t, `
apps = ["apps/server", "apps/worker"]

[secrets]
resolver = "1password"
`)
	want := []string{"apps/server", "apps/worker"}
	if len(fc.apps) != len(want) {
		t.Fatalf("apps = %v, want %v", fc.apps, want)
	}
	for i, a := range want {
		if fc.apps[i] != a {
			t.Errorf("apps[%d] = %q, want %q", i, fc.apps[i], a)
		}
	}
}

func TestFlattenIgnoresUnselectedResolver(t *testing.T) {
	// vault belongs to 1password; with aws-secret-manager selected it must not leak.
	fc := loadFrom(t, `
[secrets]
resolver = "aws-secret-manager"

[secrets.aws-secret-manager]
region = "us-east-1"

[secrets.1password]
vault = "Engineering"
`)
	if _, ok := fc.flags["vault"]; ok {
		t.Errorf("vault leaked from unselected 1password sub-table: %v", fc.flags)
	}
	if fc.flags["region"] != "us-east-1" {
		t.Errorf("region = %q, want us-east-1", fc.flags["region"])
	}
}

func TestApplyConfigDefaults(t *testing.T) {
	fc := loadFrom(t, `
apps-dir = "services"

[secrets]
resolver = "aws-secret-manager"

[secrets.aws-secret-manager]
region = "us-east-1"
`)

	cmd := &cobra.Command{Use: "run"}
	var secretResolver, region, appsDir string
	cmd.Flags().StringVar(&secretResolver, "secret-resolver", "", "")
	cmd.Flags().StringVar(&region, "region", "", "")
	cmd.Flags().StringVar(&appsDir, "apps-dir", "apps", "")

	// Simulate --apps-dir passed on the command line; the file must not override it.
	if err := cmd.Flags().Set("apps-dir", "cli-apps"); err != nil {
		t.Fatal(err)
	}

	if err := applyConfigDefaults(cmd, fc); err != nil {
		t.Fatal(err)
	}
	if secretResolver != "aws-secret-manager" {
		t.Errorf("secret-resolver = %q, want aws-secret-manager", secretResolver)
	}
	if region != "us-east-1" {
		t.Errorf("region = %q, want us-east-1", region)
	}
	if appsDir != "cli-apps" {
		t.Errorf("apps-dir = %q, want cli-apps (command line wins)", appsDir)
	}
}

func TestEffectiveFromFile(t *testing.T) {
	fc := loadFrom(t, "[secrets]\nresolver = \"1password\"\n\n[secrets.1password]\nvault = \"Engineering\"\n")
	if got := fc.effective("secret-resolver"); got != "1password" {
		t.Errorf("effective = %q, want 1password (from file)", got)
	}
	if got := fc.effective("missing"); got != "" {
		t.Errorf("effective(missing) = %q, want empty", got)
	}
}

func TestLoadConfigMissingFileIsEmpty(t *testing.T) {
	t.Chdir(t.TempDir())
	fc, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if len(fc.flags) != 0 || len(fc.apps) != 0 {
		t.Errorf("expected empty config for a missing file, got %+v", fc)
	}
}
