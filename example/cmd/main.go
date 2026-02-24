package main

import (
	"log"

	"github.com/charlesonunze/fw"
	"github.com/charlesonunze/fw/example/internal/modules/order"
	"github.com/charlesonunze/fw/example/internal/modules/user"
)

func main() {
	app := fw.New(
		fw.WithAddr(":8080"),
		fw.WithConfigPath("config.yaml"),
	)

	app.Register(
		user.New(),
		order.New(),
	)

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
