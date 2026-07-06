package cli

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	secretVals := map[string]string{
		"DB_POOL_SIZE": "s3cr3t-oops",
		"API_TOKEN":    "abc123",
		"EMPTY":        "",
	}

	msg := `parse error on field "PoolSize" of type "int": strconv.ParseInt: parsing "s3cr3t-oops": invalid syntax`
	got := redactSecrets(msg, secretVals)

	if strings.Contains(got, "s3cr3t-oops") {
		t.Errorf("value leaked, got: %q", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Errorf("missing placeholder, got: %q", got)
	}
}

func TestRedactSecretsLongestFirst(t *testing.T) {
	// One value is a substring of another; the longer must be masked whole so no
	// fragment survives.
	secretVals := map[string]string{
		"SHORT": "abc",
		"LONG":  "abc123",
	}
	got := redactSecrets(`bad values abc123 and abc here`, secretVals)
	if strings.Contains(got, "abc123") {
		t.Errorf("long value leaked, got: %q", got)
	}
}

func TestRedactSecretsEmptyMap(t *testing.T) {
	msg := "some error"
	if got := redactSecrets(msg, nil); got != msg {
		t.Errorf("nil map altered message: %q", got)
	}
}
