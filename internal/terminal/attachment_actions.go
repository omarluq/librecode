package terminal

import "github.com/gdamore/tcell/v3"

func (app *App) handleAttachmentKey(event *tcell.EventKey) bool {
	switch {
	case app.keys.matches(event, actionAttachmentPasteImage):
		return app.pasteClipboardImage()
	case app.keys.matches(event, actionAttachmentRemoveLast):
		if len(app.composerImages) == 0 {
			return false
		}

		app.composerImages = app.composerImages[:len(app.composerImages)-1]
		app.setStatus("removed last image attachment")

		return true
	case app.keys.matches(event, actionAttachmentClear):
		if len(app.composerImages) == 0 {
			return false
		}

		app.composerImages = nil
		app.setStatus("cleared image attachments")

		return true
	default:
		return false
	}
}

func (app *App) pasteClipboardImage() bool {
	if app.imageClipboard == nil {
		return false
	}

	data, err := app.imageClipboard.ReadImage()
	if err != nil {
		app.setStatus(err.Error())

		return true
	}
	// An empty image clipboard is deliberately not consumed: the terminal may
	// immediately deliver an ordinary bracketed text paste for the same key.
	if len(data) == 0 {
		return false
	}

	attachment, err := validateClipboardPNG(data, app.composerImages)
	if err != nil {
		app.setStatus(err.Error())

		return true
	}

	app.composerImages = append(app.composerImages, attachment)
	app.setStatus("attached " + attachment.Name)

	return true
}
