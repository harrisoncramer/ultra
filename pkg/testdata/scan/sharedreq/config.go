package sharedreq

// Shared is embedded through two fields carrying different required scopes. Its
// TOKEN is required in the union of those scopes.
type Shared struct {
	Token string `env:"SHARED_TOKEN" secret:"true"`
}

type Config struct {
	Optional Shared
	Prod     Shared `required:"production"`
}
