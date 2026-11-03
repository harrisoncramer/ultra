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

func TestSessionGateWarmsOnlyOnAuthenticatedResult(t *testing.T) {
	type testCase struct {
		name     string
		res      opResult
		wantWarm bool
	}
	tests := []testCase{
		{
			name: "auth failure stays cold",
			res: opResult{
				err:    errors.New("exit status 1"),
				stderr: "[ERROR] connecting to desktop app: timed out",
			},
			wantWarm: false,
		},
		{
			name: "missing item warms",
			res: opResult{
				err:    errors.New("exit status 1"),
				stderr: `"axle" isn't an item in the "Employee" vault.`,
			},
			wantWarm: true,
		},
		{
			name:     "success warms",
			res:      opResult{},
			wantWarm: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var g sessionGate
			g.do(func() opResult {
				return tt.res
			})
			assert.Equal(t, tt.wantWarm, g.warm.Load())
		})
	}
}

func TestSessionGateHoldsCallsUntilFirstAuthenticates(t *testing.T) {
	var g sessionGate
	entered := make(chan struct{})
	release := make(chan struct{})
	go g.do(func() opResult {
		close(entered)
		<-release
		return opResult{}
	})
	<-entered

	done := make(chan struct{})
	go func() {
		g.do(func() opResult {
			return opResult{}
		})
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("second call ran while the first held the gate")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-done
}

func TestSessionGateRunsCallsConcurrentlyOnceWarm(t *testing.T) {
	var g sessionGate
	g.do(func() opResult {
		return opResult{}
	})

	overlapped := make(chan struct{})
	var arrived atomic.Int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			g.do(func() opResult {
				if arrived.Add(1) == 2 {
					close(overlapped)
				}
				select {
				case <-overlapped:
				case <-time.After(time.Second):
					t.Error("warm calls did not run concurrently")
				}
				return opResult{}
			})
		})
	}
	wg.Wait()
}
