package assistant

import "github.com/samber/oops"

func assistantError(err error, action string) error {
	if err == nil {
		return nil
	}

	return oops.In("assistant").Wrapf(err, "%s", action)
}
