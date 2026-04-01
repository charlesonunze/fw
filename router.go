package fw

import (
	"net/http"

	chi "github.com/go-chi/chi/v5"
)

// Router is fw's transport-agnostic HTTP routing interface.
// The default implementation wraps chi.
// Use WithRouter() to provide your own (gin, echo, etc.)
// or ChiRouter() to bring a pre-configured chi router.
type Router interface {
	http.Handler
	Get(path string, h http.HandlerFunc)
	Post(path string, h http.HandlerFunc)
	Put(path string, h http.HandlerFunc)
	Delete(path string, h http.HandlerFunc)
	Patch(path string, h http.HandlerFunc)
	Handle(method, path string, h http.HandlerFunc)
	Group(prefix string, middleware ...func(http.Handler) http.Handler) Router
	Use(middleware ...func(http.Handler) http.Handler)
	Mount(pattern string, h http.Handler)
}

// ChiRouter wraps a pre-configured chi.Router as an fw.Router.
// Use this when you want to configure chi (middleware, options) before
// handing it to fw.
//
//	r := chi.NewRouter()
//	r.Use(otelchi.Middleware("my-service"))
//	app := fw.New(fw.WithRouter(fw.ChiRouter(r)))
func ChiRouter(r chi.Router) Router {
	return &chiRouter{r: r}
}

// chiRouter is the default Router implementation backed by chi.
type chiRouter struct {
	r chi.Router
}

func newChiRouter() *chiRouter {
	return &chiRouter{r: chi.NewRouter()}
}

func (c *chiRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) { c.r.ServeHTTP(w, r) }

func (c *chiRouter) Get(path string, h http.HandlerFunc) { c.r.Get(path, h) }

func (c *chiRouter) Post(path string, h http.HandlerFunc) { c.r.Post(path, h) }

func (c *chiRouter) Put(path string, h http.HandlerFunc) { c.r.Put(path, h) }

func (c *chiRouter) Delete(path string, h http.HandlerFunc) { c.r.Delete(path, h) }

func (c *chiRouter) Patch(path string, h http.HandlerFunc) { c.r.Patch(path, h) }

func (c *chiRouter) Handle(method, path string, h http.HandlerFunc) {
	c.r.Method(method, path, h)
}

// Group creates a sub-router mounted at prefix, with optional middleware.
// Routes registered on the returned Router are scoped to prefix.
func (c *chiRouter) Group(prefix string, middleware ...func(http.Handler) http.Handler) Router {
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
