package terminal

import "github.com/samber/oops"

func terminalError(err error, action string) error {
	if err == nil {
		return nil
	}

	return oops.In("terminal").Wrapf(err, "%s", action)
}
