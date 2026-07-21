package onepassword

import (
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// Until the first call succeeds the gate serializes, so a cold session prompts
// only once; a warmed gate then lets calls overlap. peakConcurrency reaching >1
// only after the warm-up proves both halves.
func TestWarmGateSerializesUntilWarm(t *testing.T) {
	var g warmGate

	// Warm the gate with one successful call. A concurrent burst launched before
	// this would have been serialized behind it.
	require.NoError(t, g.run(func() error { return nil }))

	const workers = 8
	var inFlight int32
	block := make(chan struct{})
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = g.run(func() error {
				atomic.AddInt32(&inFlight, 1)
				<-block
				return nil
			})
		}()
	}

	// Wait until every worker is parked inside exec at once; a gate that
	// serialized would never let inFlight reach workers, so this would hang and
	// fail the test via timeout rather than pass falsely.
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&inFlight) == workers
	}, time.Second, time.Millisecond)
	close(block)
	wg.Wait()
}

// A cold gate whose first call fails stays cold, so the next caller is still
// serialized rather than joining a false warm.
func TestWarmGateStaysColdOnFailure(t *testing.T) {
	var g warmGate
	require.Error(t, g.run(func() error { return errors.New("op unreachable") }))
	assert.False(t, g.warmed)
}
