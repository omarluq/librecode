//go:build !linux && !darwin && !windows

package terminal

func writeSystemClipboard(string) error {
	return nil
}
