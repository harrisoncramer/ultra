package flat

type Config struct {
	Plain  string `env:"PLAIN"`
	Secret string `env:"SECRET_TOKEN" secret:"true" required:"*"`
}
