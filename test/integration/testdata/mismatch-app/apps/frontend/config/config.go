package config

type Config struct {
	Token string `env:"FE_TOKEN" secret:"true" required:"*"`
}
