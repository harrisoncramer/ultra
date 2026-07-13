package unexported

// Config mixes exported and unexported fields. caarlos0/env can only set the
// exported ones, so the unexported secret must not be reported as declared.
type Config struct {
	Public string `env:"PUBLIC_TOKEN" secret:"true"`
	hidden string `env:"HIDDEN_TOKEN" secret:"true"`
}
