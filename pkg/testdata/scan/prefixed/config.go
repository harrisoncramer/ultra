package prefixed

type DB struct {
	Host     string `env:"HOST"`
	Password string `env:"PASSWORD" secret:"true"`
}

type Cache struct {
	Addr string `env:"ADDR" secret:"true"`
}

type Embedded struct {
	Token string `env:"TOKEN" secret:"true"`
}

type Config struct {
	// Named struct field with a prefix: env.Parse reads DB_HOST, DB_PASSWORD.
	Primary DB `envPrefix:"DB_"`
	// The same struct type under a different prefix: CACHE_... must not be
	// dropped as a duplicate of the DB_ visit.
	Secondary DB `envPrefix:"REPLICA_"`
	// A non-struct-prefixed nested struct still contributes its bare names.
	Cache Cache
	// Embedded struct with a prefix stacks like env.Parse: SVC_TOKEN.
	Embedded `envPrefix:"SVC_"`
	// A plain top-level secret is unaffected.
	Root string `env:"ROOT_SECRET" secret:"true"`
}
