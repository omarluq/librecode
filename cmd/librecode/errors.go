package main

import "github.com/samber/oops"

func cliError(err error, action string) error {
	if err == nil {
		return nil
	}

	return oops.In("cli").Wrapf(err, "%s", action)
}
