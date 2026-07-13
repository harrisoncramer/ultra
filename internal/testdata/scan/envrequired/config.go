package envrequired

// Config declares required in the env tag, which ultra rejects: required-ness
// must be declared with the required tag instead.
type Config struct {
	Bad string `env:"BAD,required" secret:"true"`
}
