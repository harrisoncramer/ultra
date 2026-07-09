package onepassword

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/harrisoncramer/ultra/cli"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A missing item is the override case: pick reports it as ErrSecretNotFound so
// the override layer can fall through to the base resolver.
func TestPickMissingItemIsSecretNotFound(t *testing.T) {
	o := onePassword{vault: "Employee", item: "axle"}
	_, err := o.pick(opResult{
		err:    errors.New("exit status 1"),
		stderr: `[ERROR] 2026/07/08 19:40:01 "axle" isn't an item in the "Employee" vault. Specify the item with its UUID, name, or domain.`,
	}, []string{"DATABASE_URL"})
	require.Error(t, err)
	assert.ErrorIs(t, err, cli.ErrSecretNotFound)
}

// Any other op failure stays fatal and keeps the vault and item in the message.
func TestPickRealFailureIsFatal(t *testing.T) {
	o := onePassword{vault: "Employee", item: "axle"}
	_, err := o.pick(opResult{
		err:    errors.New("exit status 1"),
		stderr: "[ERROR] connecting to desktop app: timed out",
	}, []string{"DATABASE_URL"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, cli.ErrSecretNotFound)
	assert.ErrorContains(t, err, "axle")
	assert.ErrorContains(t, err, "Employee")
}

// On success pick returns only the requested fields, omitting ones the item
// doesn't hold.
func TestPickSelectsRequestedFields(t *testing.T) {
	raw, err := json.Marshal(opItem{Fields: []opField{
		{Label: "DATABASE_URL", Value: "postgres://db"},
		{Label: "UNREQUESTED", Value: "ignored"},
	}})
	require.NoError(t, err)

	o := onePassword{vault: "Development", item: "axle"}
	got, err := o.pick(opResult{stdout: raw}, []string{"DATABASE_URL", "ABSENT"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"DATABASE_URL": "postgres://db"}, got)
}

func TestItemNotFound(t *testing.T) {
	assert.True(t, itemNotFound(`"axle" isn't an item in the "Employee" vault.`))
	assert.False(t, itemNotFound(`"Employee" isn't a vault in this account.`))
	assert.False(t, itemNotFound("connecting to desktop app: timed out"))
}
