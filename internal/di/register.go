package di

import "github.com/samber/do/v2"

// RegisterServices registers all application services in the injector.
func RegisterServices(injector do.Injector) {
	do.Provide(injector, NewConfigService)
	do.Provide(injector, NewLoggerService)
	do.Provide(injector, NewDatabaseService)
	do.Provide(injector, NewCacheService)
	do.Provide(injector, NewPluginService)
	do.Provide(injector, NewKSQLService)
	do.Provide(injector, NewAgentService)
}
