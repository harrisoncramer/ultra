package config

type Config struct {
	Token    string `env:"SVC_TOKEN" secret:"true" required:"*"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`
}
