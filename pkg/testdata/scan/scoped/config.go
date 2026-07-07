package scoped

// Base holds fields required in every environment.
type Base struct {
	Always string `env:"ALWAYS,required,notEmpty"`
	APIKey string `env:"API_KEY,required,notEmpty" secret:"true"`
}

// ProdOnly is embedded with an envScope, so its fields inherit that scope unless
// they declare their own.
type ProdOnly struct {
	ProdToken string `env:"PROD_TOKEN" secret:"true"`    // inherits production scope
	Override  string `env:"OVERRIDE" envScope:"staging"` // overrides to staging
}

type Config struct {
	Base
	ProdOnly `envScope:"production"`
	LocalURL string `env:"LOCAL_URL" envScope:"local"`
	Optional string `env:"OPTIONAL"` // unscoped, not required anywhere
}
