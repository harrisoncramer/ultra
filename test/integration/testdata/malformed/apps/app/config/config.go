package config

type Config struct {
	Token string `env:"IT_TOKEN" secret:"true" required:"*"`
	Port  int    `env:"IT_PORT" secret:"true" required:"*"`
}
