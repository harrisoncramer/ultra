package crosssub

type Extra struct {
	Tok string `env:"SUB_TOKEN" secret:"true"`
}
