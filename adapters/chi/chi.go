// Package chi provides an fwhttp.Router adapter for the go-chi/chi router.
//
// Usage:
//
//	import fwchi "github.com/charlesonunze/fw/adapters/chi"
//
//	r := chi.NewRouter()
//	httpTransport := fwhttp.New(fwchi.NewRouter(r), fwhttp.Config{
//		Middleware: []fwhttp.Middleware{tracingMiddleware},
//	})
//	app := fw.New(fw.Config{Transports: []fw.Transport{httpTransport}})
//
// The adapter populates http.Request.Pattern with the matched Chi route
// template so outer standard HTTP middleware can use low-cardinality route
// names and metric attributes.
package chi

import (
	"context"
	"net/http"
	"strings"
	"sync"

	fwhttp "github.com/charlesonunze/fw/transport/http"
	chi "github.com/go-chi/chi/v5"
)

type chiRouter struct {
	r             chi.Router
	routeContexts sync.Pool
}

// NewRouter wraps a pre-configured chi.Router as an fwhttp.Router.
func NewRouter(r chi.Router) fwhttp.Router {
	return newRouter(r)
}

func newRouter(r chi.Router) *chiRouter {
	return &chiRouter{
		r: r,
		routeContexts: sync.Pool{New: func() any {
			return chi.NewRouteContext()
		}},
	}
}

func (c *chiRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if routeContext := chi.RouteContext(r.Context()); routeContext != nil {
		c.r.ServeHTTP(w, r)
		setRequestPattern(r, routeContext)
		return
	}

	// Chi normally stores its route context on a request copy. Owning that
	// context here lets the adapter propagate the final pattern to the caller.
	routeContext := c.routeContexts.Get().(*chi.Context)
	routeContext.Reset()
	routeContext.Routes = c.r
	routedRequest := r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeContext))
	defer func() {
		c.setResolvedRequestPattern(r, routeContext)
		routeContext.Reset()
		c.routeContexts.Put(routeContext)
	}()
	c.r.ServeHTTP(w, routedRequest)
}

func (c *chiRouter) setResolvedRequestPattern(r *http.Request, routeContext *chi.Context) {
	pattern := routeContext.RoutePattern()
	if pattern == "" || strings.HasSuffix(pattern, "/*") {
		routeContext.Reset()
		if resolved := c.r.Find(routeContext, r.Method, r.URL.Path); resolved != "" {
			pattern = resolved
		}
	}
	if pattern != "" {
		r.Pattern = pattern
	}
}

func setRequestPattern(r *http.Request, routeContext *chi.Context) {
	if pattern := routeContext.RoutePattern(); pattern != "" {
		r.Pattern = pattern
	}
}

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
	return newRouter(sub)
}

func (c *chiRouter) Use(middleware ...func(http.Handler) http.Handler) {
	for _, m := range middleware {
		c.r.Use(m)
	}
}

func (c *chiRouter) Mount(pattern string, h http.Handler) {
	c.r.Mount(pattern, h)
}
