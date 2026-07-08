package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
