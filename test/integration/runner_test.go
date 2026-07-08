//go:build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

// result is the outcome of one harness CLI invocation.
type result struct {
	output string
	exit   int
}

// ok reports whether the command exited zero.
func (r result) ok() bool { return r.exit == 0 }

// ultra runs the harness CLI with args and captures its combined output and exit
// code.
func ultra(t *testing.T, args ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, binPath, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()

	res := result{output: buf.String()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		res.exit = 0
	case errors.As(err, &exitErr):
		res.exit = exitErr.ExitCode()
	default:
		t.Fatalf("running harness %v: %v\n%s", args, err, res.output)
	}
	return res
}
