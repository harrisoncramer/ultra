package vault

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/harrisoncramer/ultra/cli"
	"github.com/harrisoncramer/ultra/internal/xstring"

	"github.com/spf13/pflag"
)

// vaultTimeout bounds a single Vault HTTP read so an unreachable or hung server
// fails the call instead of blocking indefinitely.
const vaultTimeout = 15 * time.Second

func init() {
	cli.RegisterSecretResolver(cli.SecretResolverCommand{
		Name:  "vault",
		Short: "Resolve secrets from HashiCorp Vault (KV v2) over its HTTP API",
		Long: "vault reads each app's secrets from a single HashiCorp Vault KV v2 secret,\n" +
			"one secret per app whose data keys are the env-var names, for the app\n" +
			"'worker' that is <mount>/worker with keys like GOOGLE_CLIENT_ID. --mount\n" +
			"selects the KV v2 mount (default 'secret'). It talks to Vault's HTTP API\n" +
			"directly (no vault binary required): the address comes from --address or\n" +
			"VAULT_ADDR, and the token from VAULT_TOKEN or ~/.vault-token. The standard\n" +
			"VAULT_CACERT, VAULT_CAPATH, VAULT_CLIENT_CERT/KEY, VAULT_TLS_SERVER_NAME, and\n" +
			"VAULT_SKIP_VERIFY TLS settings are honored.",
		Setup: func(fs *pflag.FlagSet) func(app string) cli.SecretResolver {
			var mount, address, namespace string
			fs.StringVar(&mount, "mount", "secret", "KV v2 mount path the app's secret lives under")
			fs.StringVar(&address, "address", "", "Vault address (defaults to VAULT_ADDR)")
			fs.StringVar(&namespace, "namespace", "", "Vault namespace (Enterprise; defaults to VAULT_NAMESPACE)")
			return func(app string) cli.SecretResolver {

				token, err := vaultToken()
				if err != nil {
					log.Fatalf("failed to get vault token: %v", err)
				}

				return NewVaultResolver(NewVaultResolverParams{
					App:       app,
					Mount:     mount,
					Token:     token,
					Address:   xstring.Coalesce(os.Getenv("VAULT_ADDR"), address),
					Namespace: xstring.Coalesce(os.Getenv("VAULT_NAMESPACE"), namespace),
				})
			}
		},
	})
}

type NewVaultResolverParams struct {
	App       string
	Mount     string
	Token     string
	Address   string
	Namespace string
}

func NewVaultResolver(params NewVaultResolverParams) vaultKV {
	if params.Address == "" {
		log.Fatalf("vault requires --address or VAULT_ADDR")
	}

	vaultSkipVerify, err := strconv.ParseBool(xstring.Coalesce("false", os.Getenv("VAULT_SKIP_VERIFY")))
	if err != nil {
		log.Fatalf("VAULT_SKIP_VERIFY was set to a non-boolean value: %v", err)
	}

	config, err := NewVaultTLSConfig(NewVaultTLSConfigParams{
		vaultSkipVerify: vaultSkipVerify,
		vaultServerName: os.Getenv("VAULT_TLS_SERVER_NAME"),
		vaultCACert:     os.Getenv("VAULT_CACERT"),
		vaultCAPath:     os.Getenv("VAULT_CAPATH"),
		vaultClientCert: os.Getenv("VAULT_CLIENT_CERT"),
		vaultClientKey:  os.Getenv("VAULT_CLIENT_KEY"),
	})
	if err != nil {
		log.Fatalf("failed to load vault TLS config: %v", err)
	}

	return vaultKV{
		app:       params.App,
		mount:     params.Mount,
		address:   params.Address,
		namespace: params.Namespace,
		token:     params.Token,
		client:    vaultHTTPClient(config),
	}
}

// vaultKV resolves secrets from HashiCorp Vault's KV v2 engine over the HTTP API,
// reading one secret per app at <mount>/<app> whose data map is the name -> value
// pairs. It uses the same VAULT_ADDR/VAULT_TOKEN environment the vault CLI would,
// so no vault binary is needed and no token is passed on the command line.
type vaultKV struct {
	app       string
	mount     string
	address   string
	namespace string
	token     string
	client    *http.Client
}

// vaultResponse mirrors the KV v2 read endpoint: the actual key/value pairs are
// nested under data.data. Each value is left raw because KV v2 stores arbitrary
// JSON per key, not just strings.
type vaultResponse struct {
	Data struct {
		Data map[string]json.RawMessage `json:"data"`
	} `json:"data"`
}

// Resolve fetches the app's secret once over the KV v2 read API and picks out the
// requested keys. A missing secret (404) is reported as ErrSecretNotFound so an
// override falls through to the base resolver while a base resolver treats it as
// fatal; a missing address/token, or an unreachable/permission-denied Vault, is
// fatal, and a missing individual key is simply omitted from the result.
func (v vaultKV) Resolve(ctx context.Context, names []string) (map[string]string, error) {

	url := fmt.Sprintf("%s/v1/%s/data/%s", strings.TrimRight(v.address, "/"), v.mount, v.app)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building vault request: %w", err)
	}

	req.Header.Set("X-Vault-Token", v.token)
	if ns := v.namespace; ns != "" {
		req.Header.Set("X-Vault-Namespace", ns)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reading %s/%s from vault: %w", v.mount, v.app, err)
	}

	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("vault read %s/%s: %s: %w", v.mount, v.app, resp.Status, cli.ErrSecretNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vault read %s/%s: %s: %s", v.mount, v.app, resp.Status, strings.TrimSpace(string(body)))
	}

	var parsed vaultResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parsing vault secret %s/%s: %w", v.mount, v.app, err)
	}

	out := make(map[string]string, len(names))
	for _, name := range names {
		raw, ok := parsed.Data.Data[name]
		if !ok {
			continue
		}
		out[name] = vaultValueString(raw)
	}
	return out, nil
}

// vaultValueString renders a KV v2 value as a string: a JSON string is unquoted,
// and any other JSON scalar or structure (number, bool, object) is kept as its
// raw JSON text so non-string secrets survive intact.
func vaultValueString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// vaultHTTPClient builds the HTTP client used for a read: a bounded timeout so a
// hung server can't block forever, and a TLS config assembled from Vault's
// standard environment so an internal CA or client certificate is honored.
func vaultHTTPClient(config *tls.Config) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = config
	return &http.Client{Timeout: vaultTimeout, Transport: tr}
}

type NewVaultTLSConfigParams struct {
	vaultServerName string
	vaultSkipVerify bool
	vaultCACert     string
	vaultCAPath     string
	vaultClientCert string
	vaultClientKey  string
}

// vaultTLSConfig honors the same TLS environment the vault CLI reads:
// VAULT_CACERT / VAULT_CAPATH for a private CA, VAULT_CLIENT_CERT / VAULT_CLIENT_KEY
// for mutual TLS, VAULT_TLS_SERVER_NAME for SNI, and VAULT_SKIP_VERIFY to disable
// verification. With none set it falls back to the system roots.
func NewVaultTLSConfig(params NewVaultTLSConfigParams) (*tls.Config, error) {

	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: params.vaultSkipVerify,
	}

	if sn := params.vaultServerName; sn != "" {
		cfg.ServerName = sn
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}

	added := false

	if params.vaultCACert != "" {
		pem, err := os.ReadFile(params.vaultCACert)
		if err != nil {
			return nil, fmt.Errorf("reading vault CA cert: %w", err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("VAULT_CACERT %s: no valid certificates", params.vaultCACert)
		}
		added = true
	}

	if params.vaultCAPath != "" {
		entries, err := os.ReadDir(params.vaultCAPath)
		if err != nil {
			return nil, fmt.Errorf("reading vault CA path: %w", err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if pem, err := os.ReadFile(filepath.Join(params.vaultCAPath, e.Name())); err == nil && pool.AppendCertsFromPEM(pem) {
				added = true
			}
		}
	}
	if added {
		cfg.RootCAs = pool
	}

	if params.vaultClientCert != "" && params.vaultClientKey != "" {
		pair, err := tls.LoadX509KeyPair(params.vaultClientCert, params.vaultClientKey)
		if err != nil {
			return nil, fmt.Errorf("loading vault client cert + client key: %w", err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	return cfg, nil
}

// vaultToken reads the token from VAULT_TOKEN, falling back to the ~/.vault-token
// file the vault CLI writes on login.
func vaultToken() (string, error) {
	if t := os.Getenv("VAULT_TOKEN"); t != "" {
		return t, nil
	}
	home, err := os.UserHomeDir()
	if err == nil {
		if b, err := os.ReadFile(filepath.Join(home, ".vault-token")); err == nil {
			if t := strings.TrimSpace(string(b)); t != "" {
				return t, nil
			}
		}
	}
	return "", fmt.Errorf("vault requires VAULT_TOKEN or a ~/.vault-token file")
}
