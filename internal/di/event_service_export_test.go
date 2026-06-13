package di

// EventBusAvailableForTest reports whether the event service exposes a bus.
func EventBusAvailableForTest(service *EventService) bool {
	return service != nil && service.Bus != nil
}
