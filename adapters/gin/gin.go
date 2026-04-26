// Package gin provides a fw.Router adapter for the gin-gonic/gin router.
//
// Usage:
//
//	import fwgin "github.com/charlesonunze/fw/adapters/gin"
//
//	e := gin.Default()
//	app := fw.New(fw.WithRouter(fwgin.NewRouter(e)))
package gin

import (
	"net/http"

	"github.com/charlesonunze/fw"
	"github.com/gin-gonic/gin"
)

type ginRouter struct {
	engine *gin.Engine
	group  gin.IRouter
}

// NewRouter wraps a pre-configured *gin.Engine as an fw.Router.
func NewRouter(e *gin.Engine) fw.Router {
	return &ginRouter{engine: e, group: e}
}

func (g *ginRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.engine.ServeHTTP(w, r)
}

func (g *ginRouter) Get(path string, h http.HandlerFunc) {
	g.group.GET(path, gin.WrapF(h))
}

func (g *ginRouter) Post(path string, h http.HandlerFunc) {
	g.group.POST(path, gin.WrapF(h))
}

func (g *ginRouter) Put(path string, h http.HandlerFunc) {
	g.group.PUT(path, gin.WrapF(h))
}

func (g *ginRouter) Delete(path string, h http.HandlerFunc) {
	g.group.DELETE(path, gin.WrapF(h))
}

func (g *ginRouter) Patch(path string, h http.HandlerFunc) {
	g.group.PATCH(path, gin.WrapF(h))
}

func (g *ginRouter) Handle(method, path string, h http.HandlerFunc) {
	g.group.Handle(method, path, gin.WrapF(h))
}

func (g *ginRouter) Group(prefix string, middleware ...func(http.Handler) http.Handler) fw.Router {
	grp := g.group.Group(prefix)
	for _, m := range middleware {
		grp.Use(adaptMiddleware(m))
	}
	return &ginRouter{engine: g.engine, group: grp}
}

func (g *ginRouter) Use(middleware ...func(http.Handler) http.Handler) {
	for _, m := range middleware {
		g.group.Use(adaptMiddleware(m))
	}
}

func (g *ginRouter) Mount(pattern string, h http.Handler) {
	g.group.Any(pattern+"/*path", gin.WrapH(h))
}

// adaptMiddleware converts a standard http middleware to a gin.HandlerFunc.
func adaptMiddleware(mw func(http.Handler) http.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c.Request = r
			c.Next()
		})).ServeHTTP(c.Writer, c.Request)
	}
}
