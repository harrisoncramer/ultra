package ultraitfixture

// The fixture depends on ultra only through the throwaway program validate
// generates, which is written at runtime. This blank import keeps the dependency
// in go.mod so `go mod tidy` doesn't prune it before that program is built.
import _ "github.com/harrisoncramer/ultra"
