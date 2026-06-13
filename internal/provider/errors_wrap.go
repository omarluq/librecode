package provider

import "github.com/samber/oops"

func providerWrap(err error, action string) error {
	if err == nil {
		return nil
	}

	return oops.In("provider").Code("provider_error").Wrapf(err, "%s", action)
}
