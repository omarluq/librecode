package tool

import "github.com/samber/oops"

func toolWrap(err error, action string) error {
	if err == nil {
		return nil
	}

	return oops.In("tool").Code("tool_error").Wrapf(err, "%s", action)
}
