package fw

// Deps holds the shared dependencies injected into every module during Init.
//
// Infrastructure (DB, broker, cache, config) is not hardcoded here — register
// those as services via app.RegisterService() and retrieve them with
// fw.GetService[T](deps.Services) inside your module's Init or service methods.
type Deps struct {
	// Logger is the structured logger. Swap the implementation with WithLogger().
	Logger Logger

	// Services is the registry for cross-module and infrastructure dependencies.
	Services *ServiceRegistry
}
