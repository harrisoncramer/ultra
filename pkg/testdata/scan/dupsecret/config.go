package dupsecret

// Base declares SHARED_URL without the secret tag; the app's own field declares
// the same env name as a secret. The name would be resolved from both the config
// map and the secret store at once, a conflict the scan rejects.
type Base struct {
	Shared string `env:"SHARED_URL"`
}

type Config struct {
	Base
	Shared string `env:"SHARED_URL" secret:"true"`
	Plain  string `env:"PLAIN"`
}
