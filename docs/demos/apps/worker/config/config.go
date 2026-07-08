package config

// Config is the worker's configuration contract: one struct, the single source
// of truth for every value the app needs to boot.
type Config struct {
	DatabaseURL string `env:"DATABASE_URL" secret:"true" required:"*"`
	StripeKey   string `env:"STRIPE_SECRET_KEY" secret:"true" required:"*"`
	LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`
}
