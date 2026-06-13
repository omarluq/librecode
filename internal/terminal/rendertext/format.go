package rendertext

import "github.com/omarluq/librecode/internal/tui"

// Int formats an integer for terminal display.
func Int(value int) string { return tui.Int(value) }
