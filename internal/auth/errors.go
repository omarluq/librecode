package auth

import "github.com/samber/oops"

func authError(err error, action string) error {
	if err == nil {
		return nil
	}

	return oops.In("auth").Code("auth_error").Wrapf(err, "%s", action)
}
