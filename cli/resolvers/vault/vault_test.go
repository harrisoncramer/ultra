package vault

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/harrisoncramer/ultra/cli"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A 404 is the override case: Vault has no secret for this app, so Resolve
// reports ErrSecretNotFound and the override layer falls through to the base
// resolver rather than failing the run.
func TestResolveMissingSecretIsSecretNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[]}`))
	}))
	defer srv.Close()

	resolver := NewVaultResolver(NewVaultResolverParams{
		App:     "axle",
		Mount:   "secret",
		Token:   "test-token",
		Address: srv.URL,
	})

	_, err := resolver.Resolve(t.Context(), []string{"DATABASE_URL"})
	require.Error(t, err)
	assert.ErrorIs(t, err, cli.ErrSecretNotFound)
}

type vaultResolveCase struct {
	name       string
	body       string // JSON the KV v2 read endpoint returns
	status     int    // HTTP status to reply with (0 means 200)
	names      []string
	wantErr    bool
	wantValues map[string]string // keys that must be present with these values
	wantAbsent []string          // keys that must not be present
	wantPath   string            // expected request path ("" skips the check)
	wantToken  string            // expected X-Vault-Token ("" skips the check)
}

func TestVaultResolve(t *testing.T) {
	cases := []vaultResolveCase{
		{
			name:       "maps requested keys, omits missing and unrequested",
			body:       `{"data":{"data":{"DATABASE_URL":"postgres://x","GOOGLE_CLIENT_ID":"abc"}}}`,
			names:      []string{"DATABASE_URL", "MISSING"},
			wantValues: map[string]string{"DATABASE_URL": "postgres://x"},
			wantAbsent: []string{"MISSING", "GOOGLE_CLIENT_ID"},
			wantPath:   "/v1/secret/data/worker",
			wantToken:  "test-token",
		},
		{
			name:  "renders non-string values, keeps empty strings",
			body:  `{"data":{"data":{"PORT":8080,"DEBUG":true,"NAME":"worker","EMPTY":""}}}`,
			names: []string{"PORT", "DEBUG", "NAME", "EMPTY"},
			wantValues: map[string]string{
				"PORT":  "8080",
				"DEBUG": "true",
				"NAME":  "worker",
				"EMPTY": "",
			},
		},
		{
			name:    "non-200 status is fatal",
			body:    `{"errors":["permission denied"]}`,
			status:  http.StatusForbidden,
			names:   []string{"DATABASE_URL"},
			wantErr: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotPath, gotToken string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotToken = r.Header.Get("X-Vault-Token")
				w.Header().Set("Content-Type", "application/json")
				if c.status != 0 {
					w.WriteHeader(c.status)
				}
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()

			resolver := NewVaultResolver(NewVaultResolverParams{
				App:     "worker",
				Mount:   "secret",
				Token:   "test-token",
				Address: srv.URL,
			})

			got, err := resolver.Resolve(t.Context(), c.names)
			if c.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if c.wantPath != "" {
				assert.Equal(t, c.wantPath, gotPath)
			}
			if c.wantToken != "" {
				assert.Equal(t, c.wantToken, gotToken)
			}
			for k, want := range c.wantValues {
				assert.Contains(t, got, k)
				assert.Equal(t, want, got[k])
			}
			for _, k := range c.wantAbsent {
				assert.NotContains(t, got, k)
			}
		})
	}
}

type vaultTLSCase struct {
	name    string
	setup   func(t *testing.T, srv *httptest.Server)
	wantErr bool
}

func TestVaultResolveTLS(t *testing.T) {
	cases := []vaultTLSCase{
		{
			name:    "untrusted CA fails verification",
			setup:   func(*testing.T, *httptest.Server) {},
			wantErr: true,
		},
		{
			name: "VAULT_CACERT trusts the server",
			setup: func(t *testing.T, srv *httptest.Server) {
				caFile := filepath.Join(t.TempDir(), "ca.pem")
				block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
				require.NoError(t, os.WriteFile(caFile, block, 0o600))
				t.Setenv("VAULT_CACERT", caFile)
			},
			wantErr: false,
		},
		{
			name: "VAULT_SKIP_VERIFY bypasses verification",
			setup: func(t *testing.T, _ *httptest.Server) {
				t.Setenv("VAULT_SKIP_VERIFY", "true")
			},
			wantErr: false,
		},
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"data":{"DATABASE_URL":"postgres://x"}}}`))
	}))
	defer srv.Close()

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.setup(t, srv)

			resolver := NewVaultResolver(NewVaultResolverParams{
				App:     "worker",
				Mount:   "secret",
				Token:   "test-token",
				Address: srv.URL,
			})

			got, err := resolver.Resolve(t.Context(), []string{"DATABASE_URL"})
			if c.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "postgres://x", got["DATABASE_URL"])
		})
	}
}
