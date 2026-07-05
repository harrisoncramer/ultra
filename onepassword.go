package ultra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// OnePasswordSecretResolver resolves secrets from a 1Password vault item via the
// `op` CLI. All of an item's fields are fetched in one `op item get` call, then
// matched to the requested names by field label. It rides the local op
// desktop-app session, so no service-account token is needed. Swap it for
// another Resolver to move to a different store.
type OnePasswordSecretResolver struct {
	vault string
	item  string
}

// NewOnePasswordSecretResolverParams configures a OnePasswordSecretResolver.
type NewOnePasswordSecretResolverParams struct {
	// Vault is the 1Password vault holding the item.
	Vault string
	// Item is the vault item whose fields hold the secret values, one field per
	// secret name (matched by field label).
	Item string
}

// NewOnePasswordSecretResolver builds a resolver that reads secrets from the
// given vault item.
func NewOnePasswordSecretResolver(params NewOnePasswordSecretResolverParams) OnePasswordSecretResolver {
	return OnePasswordSecretResolver{vault: params.Vault, item: params.Item}
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
func (o OnePasswordSecretResolver) Resolve(ctx context.Context, names []string) (map[string]string, error) {
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
