// Package gin connects native Gin HTTP modules to fw's HTTP transport.
package gin

import (
	"errors"
	"net/http"

	"github.com/charlesonunze/fw"
	fwhttp "github.com/charlesonunze/fw/transport/http"
	"github.com/gin-gonic/gin"
)

// Module exposes native Gin routes. Handlers and middleware use gin.Context.
type Module interface {
	RegisterRoutes(gin.IRouter)
}

// Adapter registers module routes on a caller-configured Gin engine.
type Adapter struct {
	engine *gin.Engine
}

// NewAdapter connects engine to an fw HTTP transport.
func NewAdapter(engine *gin.Engine) *Adapter {
	return &Adapter{engine: engine}
}

func (a *Adapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.engine.ServeHTTP(w, r)
}

// RegisterModules registers native Gin modules and rejects other HTTP modules.
func (a *Adapter) RegisterModules(modules []fw.Module) error {
	if a == nil || a.engine == nil {
		return errors.New("gin adapter requires an engine")
	}
	return fwhttp.RegisterModules[gin.IRouter](modules, a.engine)
}

// RegisterHealth exposes the HTTP transport's health handlers.
func (a *Adapter) RegisterHealth(live, ready http.HandlerFunc) {
	a.engine.GET("/health/live", gin.WrapF(live))
	a.engine.GET("/health/ready", gin.WrapF(ready))
}

var _ fwhttp.Adapter = (*Adapter)(nil)
