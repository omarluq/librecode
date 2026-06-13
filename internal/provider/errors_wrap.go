package provider

import "github.com/samber/oops"

func providerWrap(err error, action string) error {
	if err == nil {
		return nil
	}

	return oops.In("provider").Wrapf(err, "%s", action)
}
