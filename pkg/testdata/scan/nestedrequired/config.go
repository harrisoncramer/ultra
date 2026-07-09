package nestedrequired

// Dev is a named (non-embedded) nested config reached through the Dev field.
// The required tag on that field must propagate to these fields, and each
// field's name must carry the field's envPrefix.
type Dev struct {
	Endpoint string `env:"ENDPOINT"`                 // inherits the parent field's required
	Debug    bool   `env:"DEBUG" required:"staging"` // own required overrides the inherited one
}

type Config struct {
	Dev   Dev    `envPrefix:"DEV_" required:"local"`
	Plain string `env:"PLAIN"` // outside the nested struct: never required
}
