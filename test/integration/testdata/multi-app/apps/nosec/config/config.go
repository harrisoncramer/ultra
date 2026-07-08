package config

type Config struct {
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
}
