package main

import (
	"os"

	"github.com/harrisoncramer/ultra/cli"

	_ "github.com/harrisoncramer/ultra/cli/resolvers/aws"
	_ "github.com/harrisoncramer/ultra/cli/resolvers/onepassword"
	_ "github.com/harrisoncramer/ultra/cli/resolvers/vault"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
