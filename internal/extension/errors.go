package extension

import "github.com/samber/oops"

func extensionError(err error, action string) error {
	if err == nil {
		return nil
	}

	return oops.In("extension").Code("extension_error").Wrapf(err, "%s", action)
}
