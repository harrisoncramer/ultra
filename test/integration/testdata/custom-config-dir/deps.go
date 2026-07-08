package ultraitcfgdir

// Present so `go mod tidy` keeps the ultra dependency; gen only scans config, but
// keeping the fixtures uniform avoids surprises if a scenario later runs validate.
import _ "github.com/harrisoncramer/ultra"
