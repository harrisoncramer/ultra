package scoped

// Base holds fields required in every environment.
type Base struct {
	Always string `env:"ALWAYS" required:"*"`
	APIKey string `env:"API_KEY" secret:"true" required:"*"`
}

// ProdOnly is embedded with a required tag, so its fields inherit those
// environments unless they declare their own.
type ProdOnly struct {
	ProdToken string `env:"PROD_TOKEN" secret:"true"`     // inherits production
	Override  string `env:"OVERRIDE" required:"staging"`  // own required overrides the inherited one
}

type Config struct {
	Base
	ProdOnly `required:"production"`
	LocalURL string `env:"LOCAL_URL" required:"local"`
	Optional string `env:"OPTIONAL"` // no required tag: never required
}
