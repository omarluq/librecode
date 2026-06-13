package model

import "github.com/samber/oops"

func modelError(err error, action string) error {
	if err == nil {
		return nil
	}

	return oops.In("model").Code("model_error").Wrapf(err, "%s", action)
}
