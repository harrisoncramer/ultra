package crosspkg

import "github.com/harrisoncramer/ultra/internal/testdata/scan/crosssub"

type Config struct {
	crosssub.Extra
	Local string `env:"LOCAL_TOKEN" secret:"true"`
}
