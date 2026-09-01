// Package chi provides an fwhttp.Router adapter for the go-chi/chi router.
//
// Usage:
//
//	import fwchi "github.com/charlesonunze/fw/adapters/chi"
//
//	r := chi.NewRouter()
//	r.Use(otelchi.Middleware("my-service"))
//	httpTransport := fwhttp.New(fwhttp.Config{Router: fwchi.NewRouter(r)})
//	app := fw.New(fw.WithTransport(httpTransport))
package chi

import (
	"net/http"

	fwhttp "github.com/charlesonunze/fw/transport/http"
	chi "github.com/go-chi/chi/v5"
)

type chiRouter struct {
	r chi.Router
}

// NewRouter wraps a pre-configured chi.Router as an fwhttp.Router.
func NewRouter(r chi.Router) fwhttp.Router {
	return &chiRouter{r: r}
}

func (c *chiRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) { c.r.ServeHTTP(w, r) }

func (c *chiRouter) Get(path string, h http.HandlerFunc)    { c.r.Get(path, h) }
func (c *chiRouter) Post(path string, h http.HandlerFunc)   { c.r.Post(path, h) }
func (c *chiRouter) Put(path string, h http.HandlerFunc)    { c.r.Put(path, h) }
func (c *chiRouter) Delete(path string, h http.HandlerFunc) { c.r.Delete(path, h) }
func (c *chiRouter) Patch(path string, h http.HandlerFunc)  { c.r.Patch(path, h) }

func (c *chiRouter) Handle(method, path string, h http.HandlerFunc) {
	c.r.Method(method, path, h)
}

func (c *chiRouter) Group(prefix string, middleware ...func(http.Handler) http.Handler) fwhttp.Router {
	sub := chi.NewRouter()
	for _, m := range middleware {
		sub.Use(m)
	}
	c.r.Mount(prefix, sub)
	return &chiRouter{r: sub}
}

func (c *chiRouter) Use(middleware ...func(http.Handler) http.Handler) {
	for _, m := range middleware {
		c.r.Use(m)
	}
}

func (c *chiRouter) Mount(pattern string, h http.Handler) {
	c.r.Mount(pattern, h)
}
