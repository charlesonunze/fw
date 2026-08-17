package main

import (
	"log"
	"net/http"

	"github.com/charlesonunze/fw"
	fwchi "github.com/charlesonunze/fw/adapters/chi"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/inventory"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/notification"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/order"
	"github.com/charlesonunze/fw/examples/ecomm/internal/modules/user"
	"github.com/go-chi/chi/v5"
)

func main() {
	router := chi.NewRouter()
	app := fw.New(
		fw.WithAddr(":8080"),
		fw.WithGRPCAddr(":9090"),
		fw.WithRouter(fwchi.NewRouter(router)),
	)

	app.Use(func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("Received %s request for %s", r.Method, r.URL.Path)
			h.ServeHTTP(w, r)
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
