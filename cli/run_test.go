package cli

import "testing"

func TestAppConfigDir(t *testing.T) {
	cases := []struct {
		root, appPath, configDir, want string
	}{
		{".", "apps/worker", "config", "apps/worker/config"},
		{".", "apps/worker", "", "apps/worker/config"},
		{".", "apps/axle", "pkg/config", "apps/axle/pkg/config"},
		{"/repo", "apps/axle", "pkg/config", "/repo/apps/axle/pkg/config"},
		{"/repo", "/abs/axle", "pkg/config", "/abs/axle/pkg/config"},
	}
	for _, c := range cases {
		if got := appConfigDir(c.root, c.appPath, c.configDir); got != c.want {
			t.Errorf("appConfigDir(%q,%q,%q) = %q, want %q", c.root, c.appPath, c.configDir, got, c.want)
		}
	}
}

func TestAppName(t *testing.T) {
	for path, want := range map[string]string{
		"apps/worker":     "worker",
		"apps/axle":       "axle",
		"/repo/apps/axle": "axle",
	} {
		if got := appName(path); got != want {
			t.Errorf("appName(%q) = %q, want %q", path, got, want)
		}
	}
}
