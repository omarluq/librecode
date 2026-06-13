package limitio

import "github.com/samber/oops"

func limitError(err error, action string) error {
	if err == nil {
		return nil
	}

	return oops.In("limitio").Code("limitio_error").Wrapf(err, "%s", action)
}
