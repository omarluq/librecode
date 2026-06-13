package browser

import "github.com/samber/oops"

func browserError(err error, action string) error {
	if err == nil {
		return nil
	}

	return oops.In("browser").Code("browser_error").Wrapf(err, "%s", action)
}
