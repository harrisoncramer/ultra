package composed

type Base struct {
	A string `env:"A_TOKEN" secret:"true"`
}

type Nested struct {
	B string `env:"B_TOKEN" secret:"true"`
}

type Config struct {
	Base
	Extra  Nested
	Direct string `env:"C_TOKEN" secret:"true"`
	Skip   string `env:"NOT_SECRET"`
}
