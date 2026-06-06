package model_test

import "github.com/omarluq/librecode/internal/model"

func disabledDiscovery() model.DiscoveryOptions {
	return model.DiscoveryOptions{
		Client:       nil,
		CachePath:    "",
		SourceURL:    "",
		CacheTTL:     0,
		FetchTimeout: 0,
		Enabled:      false,
	}
}
