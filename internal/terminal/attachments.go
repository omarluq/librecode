package terminal

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/png" // Register PNG for clipboard image configuration validation.
	"slices"
	"strings"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

const (
	clipboardImageMIME     = "image/png"
	maxComposerImages      = 4
	maxComposerImageBytes  = 5 << 20
	maxComposerImageTotal  = 20 << 20
	maxComposerImagePixels = 40_000_000
)

type imageAttachment struct {
	Name     string
	MIMEType string
	Data     []byte
	Width    int
	Height   int
}

type attachmentSummaries []attachmentSummary

type attachmentSummary struct {
	Name     string
	MIMEType string
	Width    int
	Height   int
	Size     int
}

type promptDraft struct {
	Text   string
	Images []imageAttachment
}

func (draft promptDraft) empty() bool {
	return strings.TrimSpace(draft.Text) == "" && len(draft.Images) == 0
}

func cloneImageAttachments(images []imageAttachment) []imageAttachment {
	cloned := make([]imageAttachment, len(images))
	for index := range images {
		cloned[index] = images[index]
		cloned[index].Data = bytes.Clone(images[index].Data)
	}

	return cloned
}

func clonePromptDraft(draft promptDraft) promptDraft {
	return promptDraft{Text: draft.Text, Images: cloneImageAttachments(draft.Images)}
}

func cloneImageAttachmentGroups(groups [][]imageAttachment) [][]imageAttachment {
	cloned := make([][]imageAttachment, len(groups))
	for index := range groups {
		cloned[index] = cloneImageAttachments(groups[index])
	}

	return cloned
}

func clonePromptDrafts(drafts []promptDraft) []promptDraft {
	cloned := make([]promptDraft, len(drafts))
	for index := range drafts {
		cloned[index] = clonePromptDraft(drafts[index])
	}

	return cloned
}

func promptDraftFromSteeringConsumed(event assistant.SteeringConsumedEvent) promptDraft {
	images := make([]imageAttachment, len(event.Images))
	for index, item := range event.Images {
		images[index] = imageAttachment{
			Name: item.Name, MIMEType: item.MIMEType, Data: bytes.Clone(item.Data),
			Width: item.Width, Height: item.Height,
		}
	}

	return promptDraft{Text: event.Text, Images: images}
}

func (draft promptDraft) assistantImages() []assistant.ImageAttachment {
	images := make([]assistant.ImageAttachment, len(draft.Images))
	for index, item := range draft.Images {
		images[index] = assistant.ImageAttachment{
			Name: item.Name, MIMEType: item.MIMEType, Data: bytes.Clone(item.Data),
			Width: item.Width, Height: item.Height,
		}
	}

	return images
}

func summarizeAttachments(images []imageAttachment) *attachmentSummaries {
	result := make([]attachmentSummary, len(images))
	for index, item := range images {
		result[index] = attachmentSummary{
			Name: item.Name, MIMEType: item.MIMEType, Width: item.Width,
			Height: item.Height, Size: len(item.Data),
		}
	}

	summaries := attachmentSummaries(result)

	return &summaries
}

func imageAttachmentsFromDatabase(parts []database.MessagePartEntity) []imageAttachment {
	images := make([]imageAttachment, 0, len(parts))
	for index := range parts {
		part := &parts[index]
		if part.Type == database.MessagePartImage {
			images = append(images, imageAttachment{
				Name: part.Name, MIMEType: part.MIMEType, Data: bytes.Clone(part.Data),
				Width: part.Width, Height: part.Height,
			})
		}
	}

	return images
}

func databaseAttachmentSummaries(parts []database.MessagePartEntity) *attachmentSummaries {
	result := make([]attachmentSummary, 0, len(parts))
	for _, part := range parts {
		if part.Type == database.MessagePartImage {
			result = append(result, attachmentSummary{
				Name: part.Name, MIMEType: part.MIMEType, Width: part.Width,
				Height: part.Height, Size: len(part.Data),
			})
		}
	}

	summaries := attachmentSummaries(result)

	return &summaries
}

func (app *App) composerDraftEmpty() bool {
	return app.composerBuffer.Empty() && len(app.composerImages) == 0
}

func (app *App) currentDraft() promptDraft {
	return promptDraft{
		Text:   strings.TrimSpace(app.composerBuffer.TextValue()),
		Images: cloneImageAttachments(app.composerImages),
	}
}

func (app *App) consumeDraft() promptDraft {
	draft := promptDraft{Text: strings.TrimSpace(app.composerBuffer.Clear()), Images: app.composerImages}
	app.composerImages = nil

	return draft
}

func (app *App) restoreDraft(draft promptDraft) {
	app.composerBuffer.SetText(draft.Text)
	app.composerImages = cloneImageAttachments(draft.Images)
}

func validateClipboardPNG(data []byte, existing []imageAttachment) (imageAttachment, error) {
	if err := validateClipboardImageSize(data, existing); err != nil {
		return imageAttachment{}, err
	}

	return decodeClipboardPNG(data, len(existing)+1)
}

func validateClipboardImageSize(data []byte, existing []imageAttachment) error {
	if len(existing) >= maxComposerImages {
		return fmt.Errorf("image attachment limit is %d", maxComposerImages)
	}

	if len(data) > maxComposerImageBytes {
		return errors.New("image exceeds the 5 MiB limit")
	}

	if len(data) == 0 {
		return errors.New("clipboard contains no image")
	}

	total := len(data)
	for _, item := range existing {
		total += len(item.Data)
	}

	if total > maxComposerImageTotal {
		return errors.New("image attachments exceed the 20 MiB limit")
	}

	return nil
}

func decodeClipboardPNG(data []byte, sequence int) (imageAttachment, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return imageAttachment{}, fmt.Errorf("decode clipboard image: %w", err)
	}

	if format != "png" {
		return imageAttachment{}, errors.New("clipboard image must be PNG")
	}

	if config.Width <= 0 || config.Height <= 0 || config.Width > maxComposerImagePixels/config.Height {
		return imageAttachment{}, errors.New("image dimensions must be positive and at most 40 megapixels")
	}

	return imageAttachment{
		Name: fmt.Sprintf("paste-%d.png", sequence), MIMEType: clipboardImageMIME, Data: bytes.Clone(data),
		Width: config.Width, Height: config.Height,
	}, nil
}

func (app *App) selectedModelSupportsImages() bool {
	if app.models == nil {
		return true
	}

	models := app.models.All()
	for index := range models {
		candidate := &models[index]
		if candidate.Provider == app.currentProvider() && candidate.ID == app.currentModel() {
			return slices.Contains(candidate.Input, model.InputImage)
		}
	}

	return true
}

func (app *App) validateDraftModel(draft promptDraft) bool {
	if len(draft.Images) == 0 || app.selectedModelSupportsImages() {
		return true
	}

	app.setStatus("selected model does not support image input")

	return false
}

func derefAttachmentSummaries(summaries *attachmentSummaries) []attachmentSummary {
	if summaries == nil {
		return nil
	}

	return *summaries
}
