package di

import "github.com/samber/do/v2"

// RegisterServices registers all application services in the injector.
func RegisterServices(injector do.Injector) {
	do.Provide(injector, NewConfigService)
	do.Provide(injector, NewLoggerService)
	do.Provide(injector, NewEventService)
	do.Provide(injector, NewDatabaseService)
	do.Provide(injector, NewAuthService)
	do.Provide(injector, NewModelService)
	do.Provide(injector, NewCacheService)
	do.Provide(injector, NewExtensionService)
	do.Provide(injector, NewToolService)
	do.Provide(injector, NewKSQLService)
	do.Provide(injector, NewAssistantService)
}
