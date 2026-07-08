package main

// validate builds a throwaway program that imports the ultra root package, so
// its full dependency closure must be present in this module's go.sum. This
// blank import keeps it there and stops go mod tidy from pruning it.
import _ "github.com/harrisoncramer/ultra"
