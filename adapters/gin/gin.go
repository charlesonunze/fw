// Package gin provides a fw.Router adapter for the gin-gonic/gin router.
//
// Usage:
//
//	import fwgin "github.com/charlesonunze/fw/adapters/gin"
//
//	e := gin.Default()
//	app := fw.New(fw.WithHTTP(fw.HTTPConfig{Router: fwgin.NewRouter(e)}))
package gin

import (
	"net/http"

	"github.com/charlesonunze/fw"
	"github.com/gin-gonic/gin"
)

type ginRouter struct {
	engine     *gin.Engine
	group      gin.IRouter
	middleware []func(http.Handler) http.Handler
}

// NewRouter wraps a pre-configured *gin.Engine as an fw.Router.
func NewRouter(e *gin.Engine) fw.Router {
	return &ginRouter{engine: e, group: e}
}

func (g *ginRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.engine.ServeHTTP(w, r)
}

func (g *ginRouter) Get(path string, h http.HandlerFunc) {
	g.group.GET(path, g.wrap(h))
}

func (g *ginRouter) Post(path string, h http.HandlerFunc) {
	g.group.POST(path, g.wrap(h))
}

func (g *ginRouter) Put(path string, h http.HandlerFunc) {
	g.group.PUT(path, g.wrap(h))
}

func (g *ginRouter) Delete(path string, h http.HandlerFunc) {
	g.group.DELETE(path, g.wrap(h))
}

func (g *ginRouter) Patch(path string, h http.HandlerFunc) {
	g.group.PATCH(path, g.wrap(h))
}

func (g *ginRouter) Handle(method, path string, h http.HandlerFunc) {
	g.group.Handle(method, path, g.wrap(h))
}

func (g *ginRouter) Group(prefix string, middleware ...func(http.Handler) http.Handler) fw.Router {
	grp := g.group.Group(prefix)
	inherited := make([]func(http.Handler) http.Handler, 0, len(g.middleware)+len(middleware))
	inherited = append(inherited, g.middleware...)
	inherited = append(inherited, middleware...)
	return &ginRouter{engine: g.engine, group: grp, middleware: inherited}
}

func (g *ginRouter) Use(middleware ...func(http.Handler) http.Handler) {
	g.middleware = append(g.middleware, middleware...)
}

func (g *ginRouter) Mount(pattern string, h http.Handler) {
	g.group.Any(pattern+"/*path", g.wrap(h))
}

func (g *ginRouter) wrap(handler http.Handler) gin.HandlerFunc {
	for i := len(g.middleware) - 1; i >= 0; i-- {
		handler = g.middleware[i](handler)
	}
	return gin.WrapH(handler)
}
