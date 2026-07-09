package config

type DB struct {
	URL string `env:"URL" secret:"true" required:"*"`
}

type Config struct {
	Database DB     `envPrefix:"DB_"`
	APIKey   string `env:"API_KEY" secret:"true" required:"*"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
}
