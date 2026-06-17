package terminal

import "golang.design/x/clipboard"

type systemClipboard interface {
	WriteText(text string) error
}

type desktopClipboard struct{}

func (desktopClipboard) WriteText(text string) error {
	if text == "" {
		return nil
	}

	if err := clipboard.Init(); err != nil {
		return terminalError(err, "init system clipboard")
	}

	clipboard.Write(clipboard.FmtText, []byte(text))

	return nil
}
