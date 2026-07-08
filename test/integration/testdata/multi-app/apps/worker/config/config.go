package config

type Config struct {
	DatabaseURL string `env:"DATABASE_URL" secret:"true" required:"*"`
	WorkerToken string `env:"WORKER_TOKEN" secret:"true" required:"*"`
	LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`
}
