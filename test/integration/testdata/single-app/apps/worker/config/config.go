package config

type Config struct {
	DatabaseURL string `env:"IT_DB_URL" secret:"true" required:"*"`
	APIKey      string `env:"IT_API_KEY" secret:"true" required:"*"`
	LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`
}
