package cli

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestVaultResolve(t *testing.T) {
	var gotPath, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotToken = r.Header.Get("X-Vault-Token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"data":{"DATABASE_URL":"postgres://x","GOOGLE_CLIENT_ID":"abc"}}}`))
	}))
	defer srv.Close()

	t.Setenv("VAULT_TOKEN", "test-token")

	r := vaultKV{app: "worker", mount: "secret", address: srv.URL}
	got, err := r.Resolve(t.Context(), []string{"DATABASE_URL", "MISSING"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if gotPath != "/v1/secret/data/worker" {
		t.Errorf("request path = %q, want /v1/secret/data/worker", gotPath)
	}
	if gotToken != "test-token" {
		t.Errorf("X-Vault-Token = %q, want test-token", gotToken)
	}
	if got["DATABASE_URL"] != "postgres://x" {
		t.Errorf("DATABASE_URL = %q, want postgres://x", got["DATABASE_URL"])
	}
	if _, ok := got["MISSING"]; ok {
		t.Errorf("MISSING should be omitted, got %q", got["MISSING"])
	}
	if _, ok := got["GOOGLE_CLIENT_ID"]; ok {
		t.Errorf("GOOGLE_CLIENT_ID was not requested and must not be returned")
	}
}

func TestVaultResolveNonStringValues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"data":{"PORT":8080,"DEBUG":true,"NAME":"worker","EMPTY":""}}}`))
	}))
	defer srv.Close()

	t.Setenv("VAULT_TOKEN", "test-token")

	r := vaultKV{app: "worker", mount: "secret", address: srv.URL}
	got, err := r.Resolve(t.Context(), []string{"PORT", "DEBUG", "NAME", "EMPTY"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if got["PORT"] != "8080" {
		t.Errorf("PORT = %q, want 8080", got["PORT"])
	}
	if got["DEBUG"] != "true" {
		t.Errorf("DEBUG = %q, want true", got["DEBUG"])
	}
	if got["NAME"] != "worker" {
		t.Errorf("NAME = %q, want worker", got["NAME"])
	}
	if v, ok := got["EMPTY"]; !ok || v != "" {
		t.Errorf("EMPTY = %q (present=%v), want empty string present", v, ok)
	}
}

func TestVaultResolveErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":["permission denied"]}`))
	}))
	defer srv.Close()

	t.Setenv("VAULT_TOKEN", "test-token")

	r := vaultKV{app: "worker", mount: "secret", address: srv.URL}
	if _, err := r.Resolve(t.Context(), []string{"DATABASE_URL"}); err == nil {
		t.Fatal("expected error on 403, got nil")
	}
}

func TestVaultResolveCustomCA(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"data":{"DATABASE_URL":"postgres://x"}}}`))
	}))
	defer srv.Close()
	t.Setenv("VAULT_TOKEN", "test-token")

	r := vaultKV{app: "worker", mount: "secret", address: srv.URL}

	// The server's cert is signed by an untrusted CA, so a plain read must fail.
	if _, err := r.Resolve(t.Context(), []string{"DATABASE_URL"}); err == nil {
		t.Fatal("expected TLS verification failure without VAULT_CACERT")
	}

	// Writing the server's cert to VAULT_CACERT makes it trusted.
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	pem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(caFile, pem, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VAULT_CACERT", caFile)
	got, err := r.Resolve(t.Context(), []string{"DATABASE_URL"})
	if err != nil {
		t.Fatalf("Resolve with VAULT_CACERT: %v", err)
	}
	if got["DATABASE_URL"] != "postgres://x" {
		t.Errorf("DATABASE_URL = %q, want postgres://x", got["DATABASE_URL"])
	}

	// VAULT_SKIP_VERIFY also bypasses verification.
	t.Setenv("VAULT_CACERT", "")
	t.Setenv("VAULT_SKIP_VERIFY", "true")
	if _, err := r.Resolve(t.Context(), []string{"DATABASE_URL"}); err != nil {
		t.Fatalf("Resolve with VAULT_SKIP_VERIFY: %v", err)
	}
}
