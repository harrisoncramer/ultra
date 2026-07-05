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
region = "us-east-1"

[config]
resolver = "docker-compose"
`)
	cases := map[string]string{
		"secret-resolver": "aws-secret-manager",
		"region":          "us-east-1",
		"config-resolver": "docker-compose",
		"apps-dir":        "services",
	}
	for flag, want := range cases {
		if got := fc[flag]; got != want {
			t.Errorf("fc[%q] = %q, want %q", flag, got, want)
		}
	}
}

func TestApplyConfigDefaults(t *testing.T) {
	fc := loadFrom(t, `
apps-dir = "services"

[secrets]
resolver = "aws-secret-manager"
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
	fc := loadFrom(t, "[secrets]\nresolver = \"1password\"\n")
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
	if len(fc) != 0 {
		t.Errorf("expected empty config for a missing file, got %v", fc)
	}
}
