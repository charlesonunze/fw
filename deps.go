package fw

// Deps holds the dependencies injected into a module lifecycle phase.
//
// Infrastructure (DB, broker, cache, config) is not hardcoded here — register
// those as services via app.RegisterService() and retrieve them with
// fw.GetService[T](deps.Services) inside your module. A module can resolve
// application services, its own services, and services owned by modules it
// directly imports.
type Deps struct {
	// Logger is the structured logger configured on the application.
	Logger Logger

	// Services is the registry view scoped to the current module.
	Services *ServiceRegistry
}
