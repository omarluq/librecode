package assistant_test

import "github.com/omarluq/librecode/internal/model"

func disabledModelDiscovery() model.DiscoveryOptions {
	return model.DiscoveryOptions{
		Client:       nil,
		CachePath:    "",
		SourceURL:    "",
		CacheTTL:     0,
		FetchTimeout: 0,
		Enabled:      false,
	}
}
