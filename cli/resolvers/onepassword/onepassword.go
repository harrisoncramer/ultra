package onepassword

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/harrisoncramer/ultra/cli"

	"github.com/spf13/pflag"
)

// opMu serializes `op` invocations. Callers resolve apps concurrently, but the op
// CLI unlocks via a biometric prompt per invocation until its desktop-app session
// is cached; firing many at once triggers a prompt storm. Running them one at a
// time lets the first prompt cache the session so the rest reuse it silently.
var opMu sync.Mutex

func init() {
	cli.RegisterSecretResolver(cli.SecretResolverCommand{
		Name:  "1password",
		Short: "Resolve secrets from 1Password via the op CLI",
		Setup: func(fs *pflag.FlagSet) func(app string) cli.SecretResolver {
			var vault string
			fs.StringVar(&vault, "vault", "", "1password vault holding the secrets (required)")
			return func(app string) cli.SecretResolver {
				return onePassword{vault: vault, item: app}
			}
		},
	})
}

// onePassword resolves secrets from a 1Password vault item via the op CLI. All of
// an item's fields are fetched in one `op item get` call, then matched to the
// requested names by field label. It rides the local op desktop-app session, so
// no service-account token is needed.
type onePassword struct {
	vault string
	item  string
}

type opField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type opItem struct {
	Fields []opField `json:"fields"`
}

// Resolve fetches the whole item once and picks out the requested field labels.
// A missing item is reported as ErrSecretNotFound so an override falls through to
// the base resolver while a base resolver treats it as fatal; a missing vault, an
// auth failure, or an unreachable op is fatal. A missing individual field is
// simply omitted from the result.
func (o onePassword) Resolve(ctx context.Context, names []string) (map[string]string, error) {
	if o.vault == "" {
		return nil, fmt.Errorf("1password requires --vault")
	}

	cmd := exec.CommandContext(ctx, "op", "item", "get", o.item, "--vault", o.vault, "--format", "json", "--reveal")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	opMu.Lock()
	err := cmd.Run()
	opMu.Unlock()

	return o.pick(opResult{err: err, stdout: stdout.Bytes(), stderr: stderr.String()}, names)
}

// opResult is the raw outcome of one `op item get`, kept separate from the exec
// so the field selection and error classification can be tested without invoking
// the op CLI.
type opResult struct {
	err    error
	stdout []byte
	stderr string
}

// pick turns a completed op invocation into the requested secrets, reporting a
// missing item as ErrSecretNotFound and any other failure as a plain error.
func (o onePassword) pick(res opResult, names []string) (map[string]string, error) {
	if res.err != nil {
		msg := strings.TrimSpace(res.stderr)
		if msg == "" {
			msg = res.err.Error()
		}
		if itemNotFound(msg) {
			return nil, fmt.Errorf("op item get %q in vault %q: %s: %w", o.item, o.vault, msg, cli.ErrSecretNotFound)
		}
		return nil, fmt.Errorf("op item get %q in vault %q: %s", o.item, o.vault, msg)
	}

	var item opItem
	if err := json.Unmarshal(res.stdout, &item); err != nil {
		return nil, fmt.Errorf("parsing 1password item %q: %w", o.item, err)
	}

	byLabel := make(map[string]string, len(item.Fields))
	for _, f := range item.Fields {
		byLabel[f.Label] = f.Value
	}

	out := make(map[string]string, len(names))
	for _, name := range names {
		if v := byLabel[name]; v != "" {
			out[name] = v
		}
	}
	return out, nil
}

// itemNotFound reports whether op failed only because the vault has no such item,
// the case where an override does not cover this app, as opposed to a missing
// vault, an auth failure, or op being unreachable.
func itemNotFound(stderr string) bool {
	return strings.Contains(stderr, "isn't an item in the")
}
