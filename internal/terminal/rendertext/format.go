package rendertext

import "strconv"

// Int formats an integer for terminal display.
func Int(value int) string {
	return strconv.Itoa(value)
}
