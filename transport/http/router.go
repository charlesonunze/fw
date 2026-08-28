package fwhttp

import "net/http"

// Router is the HTTP routing contract used by fw modules. Router adapters
// bridge this interface to Chi, Gin, and other HTTP frameworks.
type Router interface {
	http.Handler

	Get(path string, handler http.HandlerFunc)
	Post(path string, handler http.HandlerFunc)
	Put(path string, handler http.HandlerFunc)
	Delete(path string, handler http.HandlerFunc)
	Patch(path string, handler http.HandlerFunc)
	Handle(method, path string, handler http.HandlerFunc)
	Group(prefix string, middleware ...func(http.Handler) http.Handler) Router
	Use(middleware ...func(http.Handler) http.Handler)
	Mount(pattern string, handler http.Handler)
}

// Module is implemented by application modules that expose HTTP routes.
type Module interface {
	RegisterRoutes(router Router)
}
