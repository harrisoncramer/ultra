package config

type Config struct {
	DatabaseURL string `env:"DATABASE_URL" secret:"true" required:"*"`
	ServerToken string `env:"SERVER_TOKEN" secret:"true" required:"*"`
	LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`
}
