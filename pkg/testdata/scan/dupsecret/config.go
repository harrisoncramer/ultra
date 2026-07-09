package dupsecret

// Base declares SHARED_URL without the secret tag; the app's own field declares
// the same env name as a secret. env.Parse populates both from SHARED_URL, so it
// must be treated as a secret regardless of which field the scan visits first.
type Base struct {
	Shared string `env:"SHARED_URL"`
}

type Config struct {
	Base
	Shared string `env:"SHARED_URL" secret:"true"`
	Plain  string `env:"PLAIN"`
}
