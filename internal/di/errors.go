package di

import "github.com/samber/oops"

func serviceError(err error, action string) error {
	if err == nil {
		return nil
	}

	return oops.In("di").Code("di_error").Wrapf(err, "%s", action)
}
