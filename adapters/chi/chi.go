// Package chi connects native Chi HTTP modules to fw's HTTP transport.
//
// Usage:
//
//	import fwchi "github.com/charlesonunze/fw/adapters/chi"
//
//	r := chi.NewRouter()
//	httpTransport := fwhttp.New(fwchi.NewAdapter(r), fwhttp.Config{
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
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/charlesonunze/fw"
	fwhttp "github.com/charlesonunze/fw/transport/http"
	chi "github.com/go-chi/chi/v5"
)

// Module exposes native Chi routes with standard HTTP handlers and middleware.
type Module interface {
	RegisterRoutes(chi.Router)
}

// Adapter registers module routes on a caller-configured Chi router.
type Adapter struct {
	r             chi.Router
	routeContexts sync.Pool
}

// NewAdapter connects r to an fw HTTP transport.
func NewAdapter(r chi.Router) *Adapter {
	return &Adapter{
		r: r,
		routeContexts: sync.Pool{New: func() any {
			return chi.NewRouteContext()
		}},
	}
}

func (c *Adapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

func (c *Adapter) setResolvedRequestPattern(r *http.Request, routeContext *chi.Context) {
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

// RegisterModules registers native Chi modules and rejects other HTTP modules.
func (c *Adapter) RegisterModules(modules []fw.Module) error {
	if c == nil || c.r == nil {
		return errors.New("chi adapter requires a router")
	}
	return fwhttp.RegisterModules[chi.Router](modules, c.r)
}

// RegisterHealth exposes the HTTP transport's health handlers.
func (c *Adapter) RegisterHealth(live, ready http.HandlerFunc) {
	c.r.Get("/health/live", live)
	c.r.Get("/health/ready", ready)
}

var _ fwhttp.Adapter = (*Adapter)(nil)
