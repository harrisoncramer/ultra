package sharedreq

// Shared is declared through two fields carrying different required scopes, so
// SHARED_TOKEN is declared twice from the same source: a redeclaration the scan
// rejects rather than guessing which scope applies.
type Shared struct {
	Token string `env:"SHARED_TOKEN" secret:"true"`
}

type Config struct {
	Optional Shared
	Prod     Shared `required:"production"`
}
