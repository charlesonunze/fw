package fw

import "net/http"

// Router is fw's transport-agnostic HTTP routing interface.
// Implement it yourself or use an adapter:
//
//	import fwchi "github.com/charlesonunze/fw/adapters/chi"
//	import fwgin "github.com/charlesonunze/fw/adapters/gin"
//
//	app := fw.New(fw.WithHTTP(fw.HTTPConfig{Router: fwchi.NewRouter(r)}))
//	app := fw.New(fw.WithHTTP(fw.HTTPConfig{Router: fwgin.NewRouter(e)}))
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
