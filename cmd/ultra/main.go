package main

import (
	"os"

	"github.com/harrisoncramer/ultra/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
