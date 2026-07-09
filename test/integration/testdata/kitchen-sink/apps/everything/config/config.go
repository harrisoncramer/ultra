package config

import "net/url"

// Cache is a named nested config. Its required scope is inherited from the
// field that holds it, and its env names carry that field's envPrefix.
type Cache struct {
	Host string `env:"HOST"`                   // inherits the Cache field's required:"*"
	Port int    `env:"PORT" envDefault:"6379"` // defaulted, so the inherited required is always satisfied
	TLS  bool   `env:"TLS" envDefault:"false"`
}

// Telemetry is embedded; its fields inherit the embed's required scope.
type Telemetry struct {
	OTelKey string `env:"OTEL_KEY" secret:"true"` // inherits production
}

// Config is a deliberately gnarly config: every scalar kind, a parsed URL,
// secret and non-secret fields, defaults, a named nested struct with an
// envPrefix and inherited required, an embedded struct with inherited required,
// and required scopes covering *, a single env, and none.
type Config struct {
	LogLevel   string  `env:"LOG_LEVEL" envDefault:"info"`
	Workers    int     `env:"WORKERS" envDefault:"4"`
	SampleRate float64 `env:"SAMPLE_RATE" envDefault:"0.1"`
	Debug      bool    `env:"DEBUG" envDefault:"false"`

	CDNURL url.URL `env:"CDN_URL" required:"*"`

	APIKey     string `env:"API_KEY" secret:"true" required:"*"`
	ProdSecret string `env:"PROD_SECRET" secret:"true" required:"production"`

	LocalOnly string `env:"LOCAL_ONLY" required:"local"`
	Optional  string `env:"OPTIONAL"`

	Cache Cache `envPrefix:"CACHE_" required:"*"`

	Telemetry `required:"production"`
}
