package main

import (
	"os"

	"github.com/charlesonunze/fw/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
