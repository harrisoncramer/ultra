package config

type Config struct {
	Always    string `env:"IT_ALWAYS" secret:"true" required:"*"`
	ProdOnly  string `env:"IT_PROD" secret:"true" required:"production"`
	LocalOnly string `env:"IT_LOCAL" secret:"true" required:"local"`
	Optional  string `env:"IT_OPT"`
}
