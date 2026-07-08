// Package project models the repo layout the commands operate on: the root the
// compose file and app paths are anchored to, and where each app's config
// package lives. The run, validate and lint domains embed a Project to locate
// apps consistently.
package project

import "path/filepath"

// Project is the repo layout shared across commands.
type Project struct {
	// Root is the repo root the compose file and app paths are anchored to.
	Root string
	// ConfigDir is the config package directory under each app path (e.g.
	// "config" or "pkg/config"). Empty means "config".
	ConfigDir string
}

// AppName is the short name used to namespace an app's secrets, derived from the
// last element of its path — apps/server becomes "server".
func (p Project) AppName(appPath string) string {
	return filepath.Base(appPath)
}

// AppConfigDir is the app's config package directory: <appPath>/<ConfigDir>,
// resolved under Root unless appPath is already absolute.
func (p Project) AppConfigDir(appPath string) string {
	configDir := p.ConfigDir
	if configDir == "" {
		configDir = "config"
	}
	if filepath.IsAbs(appPath) {
		return filepath.Join(appPath, configDir)
	}
	return filepath.Join(p.Root, appPath, configDir)
}
