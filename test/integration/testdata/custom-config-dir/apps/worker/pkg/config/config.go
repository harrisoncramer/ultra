package config

type Config struct {
	APIKey string `env:"IT_API_KEY" secret:"true" required:"*"`
}
