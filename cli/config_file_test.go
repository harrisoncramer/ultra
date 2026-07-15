package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadFrom writes body to a temp config file, reads it through viper, and returns
// the flattened fileConfig.
func loadFrom(t *testing.T, body string) fileConfig {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, configFileName), []byte(body), 0o644))
	v := viper.New()
	v.SetConfigFile(filepath.Join(dir, configFileName))
	v.SetConfigType("toml")
	require.NoError(t, v.ReadInConfig())
	return flatten(v)
}

// withArgs swaps os.Args for the duration of the test.
func withArgs(t *testing.T, args []string) {
	t.Helper()
	orig := os.Args
	os.Args = args
	t.Cleanup(func() { os.Args = orig })
}

type flattenCase struct {
	name           string
	body           string
	wantFlags      map[string]string
	wantFlagAbsent []string
	wantApps       []string
}

func TestFlatten(t *testing.T) {
	cases := []flattenCase{
		{
			name: "sections map onto flag names",
			body: `
apps-dir = "services"

[secrets]
resolver = "aws-secret-manager"

[secrets.aws-secret-manager]
region = "us-east-1"
profile = "prod"

[config]
resolver = "docker-compose"
`,
			wantFlags: map[string]string{
				"secret-resolver": "aws-secret-manager",
				"region":          "us-east-1",
				"profile":         "prod",
				"config-resolver": "docker-compose",
			},
		},
		{
			name: "apps list is read out",
			body: `
apps = ["apps/server", "apps/worker"]

[secrets]
resolver = "1password"
`,
			wantApps: []string{"apps/server", "apps/worker"},
		},
		{
			name: "unselected resolver's sub-table does not leak",
			body: `
[secrets]
resolver = "aws-secret-manager"

[secrets.aws-secret-manager]
region = "us-east-1"

[secrets.1password]
vault = "Engineering"
`,
			wantFlags:      map[string]string{"region": "us-east-1"},
			wantFlagAbsent: []string{"vault"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fc := loadFrom(t, c.body)
			for flag, want := range c.wantFlags {
				assert.Equal(t, want, fc.flags[flag])
			}
			for _, flag := range c.wantFlagAbsent {
				assert.NotContains(t, fc.flags, flag)
			}
			if c.wantApps != nil {
				assert.Equal(t, c.wantApps, fc.apps)
			}
		})
	}
}

func TestFlattenListValuedFlag(t *testing.T) {
	fc := loadFrom(t, `
compose-file = ["docker-compose.yml", "docker-compose.override.yml"]

[secrets]
resolver = "1password"
`)
	assert.Equal(t, []string{"docker-compose.yml", "docker-compose.override.yml"}, fc.listFlags["compose-file"])
	// A TOML array is a list-valued flag, not a scalar, so it must not also land in
	// flags as a stringified slice.
	assert.NotContains(t, fc.flags, "compose-file")
}

func TestApplyConfigDefaultsListFlag(t *testing.T) {
	fc := loadFrom(t, "compose-file = [\"docker-compose.yml\", \"docker-compose.override.yml\"]\n")
	cmd := &cobra.Command{Use: "run"}
	var composeFiles []string
	cmd.Flags().StringArrayVar(&composeFiles, "compose-file", nil, "")

	require.NoError(t, applyConfigDefaults(cmd, fc))
	// Each array element sets the flag once, so a repeatable flag fills from the
	// file exactly as passing it repeatedly on the command line would.
	assert.Equal(t, []string{"docker-compose.yml", "docker-compose.override.yml"}, composeFiles)
}

func TestApplyConfigDefaultsListFlagCommandLineWins(t *testing.T) {
	fc := loadFrom(t, "compose-file = [\"from-file.yml\"]\n")
	cmd := &cobra.Command{Use: "run"}
	var composeFiles []string
	cmd.Flags().StringArrayVar(&composeFiles, "compose-file", nil, "")
	require.NoError(t, cmd.Flags().Set("compose-file", "from-cli.yml"))

	require.NoError(t, applyConfigDefaults(cmd, fc))
	// The flag is already Changed from the command line, so the file must not
	// append its values on top.
	assert.Equal(t, []string{"from-cli.yml"}, composeFiles)
}

type applyDefaultsCase struct {
	name   string
	body   string
	preset map[string]string // flags set on the command line before applying defaults
	want   map[string]string // expected flag values after applyConfigDefaults
}

func TestApplyConfigDefaults(t *testing.T) {
	cases := []applyDefaultsCase{
		{
			name: "file fills unset flags, command line wins",
			body: `
apps-dir = "services"

[secrets]
resolver = "aws-secret-manager"

[secrets.aws-secret-manager]
region = "us-east-1"
`,
			preset: map[string]string{"apps-dir": "cli-apps"},
			want: map[string]string{
				"secret-resolver": "aws-secret-manager",
				"region":          "us-east-1",
				"apps-dir":        "cli-apps",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fc := loadFrom(t, c.body)
			cmd := &cobra.Command{Use: "run"}
			var secretResolver, region, appsDir string
			cmd.Flags().StringVar(&secretResolver, "secret-resolver", "", "")
			cmd.Flags().StringVar(&region, "region", "", "")
			cmd.Flags().StringVar(&appsDir, "apps-dir", "apps", "")

			for flag, val := range c.preset {
				require.NoError(t, cmd.Flags().Set(flag, val))
			}
			require.NoError(t, applyConfigDefaults(cmd, fc))
			for flag, want := range c.want {
				assert.Equal(t, want, cmd.Flags().Lookup(flag).Value.String())
			}
		})
	}
}

type effectiveCase struct {
	name string
	body string
	flag string
	want string
}

func TestEffectiveFromFile(t *testing.T) {
	const body = "[secrets]\nresolver = \"1password\"\n\n[secrets.1password]\nvault = \"Engineering\"\n"
	cases := []effectiveCase{
		{"resolver comes from the file", body, "secret-resolver", "1password"},
		{"unknown flag is empty", body, "missing", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withArgs(t, []string{"ultra", "run"})
			fc := loadFrom(t, c.body)
			assert.Equal(t, c.want, fc.effective(c.flag))
		})
	}
}

type loadConfigCase struct {
	name         string
	body         string // config file contents; empty means write no file
	atConfigFlag bool   // write body at an explicit --config-file path
	explicitMiss bool   // point --config-file at a nonexistent file
	wantErr      bool
	wantFlags    map[string]string
	wantApps     []string
	wantEmpty    bool // fc must have no flags and no apps
}

func TestLoadConfig(t *testing.T) {
	cases := []loadConfigCase{
		{
			name:      "missing default file yields empty config",
			wantEmpty: true,
		},
		{
			name:         "reads an explicit --config-file",
			body:         "apps = [\"apps/server\"]\n\n[secrets]\nresolver = \"1password\"\n\n[secrets.1password]\nvault = \"Engineering\"\n",
			atConfigFlag: true,
			wantFlags:    map[string]string{"secret-resolver": "1password", "vault": "Engineering"},
			wantApps:     []string{"apps/server"},
		},
		{
			name:         "explicit missing --config-file errors",
			explicitMiss: true,
			wantErr:      true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cwd := t.TempDir()
			t.Chdir(cwd)
			args := []string{"ultra", "run"}
			switch {
			case c.atConfigFlag:
				path := filepath.Join(t.TempDir(), "custom.toml")
				require.NoError(t, os.WriteFile(path, []byte(c.body), 0o644))
				args = append(args, "--config-file", path)
			case c.explicitMiss:
				args = append(args, "--config-file", filepath.Join(t.TempDir(), "nope.toml"))
			case c.body != "":
				require.NoError(t, os.WriteFile(filepath.Join(cwd, configFileName), []byte(c.body), 0o644))
			}
			withArgs(t, args)

			fc, err := loadConfig()
			if c.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if c.wantEmpty {
				assert.Empty(t, fc.flags)
				assert.Empty(t, fc.apps)
			}
			for flag, want := range c.wantFlags {
				assert.Equal(t, want, fc.flags[flag])
			}
			if c.wantApps != nil {
				assert.Equal(t, c.wantApps, fc.apps)
			}
		})
	}
}

func TestFlattenOverrideSections(t *testing.T) {
	fc := loadFrom(t, `
[secrets]
resolver = "aws-secret-manager"

[secrets.aws-secret-manager]
region = "us-east-1"

[secrets-override]
resolver = "1password"

[secrets-override.1password]
vault = "LocalDev"

[config-override]
resolver = "env"
`)
	assert.Equal(t, "1password", fc.override.secretResolver)
	assert.Equal(t, "LocalDev", fc.override.secretFlags["vault"])
	assert.Equal(t, "env", fc.override.configResolver)
	// The override's own flags must not leak into the base resolver flags.
	assert.NotContains(t, fc.flags, "vault")
}
