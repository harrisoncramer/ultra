package onepassword

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/harrisoncramer/ultra/cli"

	"github.com/spf13/pflag"
)

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
// A missing vault or item is a fatal error; a missing individual field is simply
// omitted from the result.
func (o onePassword) Resolve(ctx context.Context, names []string) (map[string]string, error) {
	if o.vault == "" {
		return nil, fmt.Errorf("1password requires --vault")
	}

	cmd := exec.CommandContext(ctx, "op", "item", "get", o.item, "--vault", o.vault, "--format", "json", "--reveal")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("op item get %q in vault %q: %s", o.item, o.vault, msg)
	}

	var item opItem
	if err := json.Unmarshal(stdout.Bytes(), &item); err != nil {
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
