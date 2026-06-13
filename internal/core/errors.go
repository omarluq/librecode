package core

import "github.com/samber/oops"

func coreError(err error, action string) error {
	if err == nil {
		return nil
	}

	return oops.In("core").Code("core_error").Wrapf(err, "%s", action)
}
