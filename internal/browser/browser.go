// Package browser opens URLs with the user's system browser.
package browser

import (
	pkgbrowser "github.com/pkg/browser"
	"github.com/samber/oops"
)

// Open asks the operating system to open targetURL in the user's browser.
func Open(targetURL string) error {
	return open(targetURL, pkgbrowser.OpenURL)
}

type openURLFunc func(url string) error

func open(targetURL string, openURL openURLFunc) error {
	if err := openURL(targetURL); err != nil {
		return oops.In("browser").Code("browser_error").Wrapf(err, "open browser")
	}

	return nil
}
