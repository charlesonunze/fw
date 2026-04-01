package main

import (
	"log"
	"net/http"

	"github.com/charlesonunze/fw"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/inventory"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/notification"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/order"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/user"
)

func main() {
	app := fw.New(
		fw.WithAddr(":8080"),
		fw.WithGRPCAddr(":9090"),
	)

	app.Use(func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("Received %s request for %s", r.Method, r.URL.Path)
		})
	})

	app.RegisterModules(
		user.New(),
		order.New(),
		inventory.New(),
		notification.New(),
	)

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
