package main

import "github.com/samber/oops"

const (
	cliResolveDatabaseService  = "resolve database service"
	cliResolveWorkingDirectory = "resolve working directory"
)

func cliError(err error, action string) error {
	if err == nil {
		return nil
	}

	return oops.In("cli").Wrapf(err, "%s", action)
}
