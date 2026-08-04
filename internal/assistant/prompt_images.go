package assistant

import (
	"bytes"
	"image"
	_ "image/gif"  // Register supported image decoders for DecodeConfig.
	_ "image/jpeg" // Register supported image decoders for DecodeConfig.
	_ "image/png"  // Register supported image decoders for DecodeConfig.
	"slices"
	"strings"

	"github.com/samber/oops"
	_ "golang.org/x/image/webp" // Register WebP for DecodeConfig.

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

const (
	maxPromptImages      = 4
	maxPromptImageName   = 255
	maxPromptImageBytes  = 5 << 20
	maxPromptImageTotal  = 20 << 20
	maxPromptImagePixels = 40_000_000
	imageMIMEPNG         = "image/png"
	imageMIMEWebP        = "image/webp"
)

func (runtime *Runtime) preparePromptRequest(request *PromptRequest) (*PromptRequest, error) {
	if err := validatePromptImageAllocationBounds(request.Images); err != nil {
		return nil, err
	}

	cloned := clonePromptRequest(request)
	if err := runtime.validatePromptRequest(cloned); err != nil {
		return nil, err
	}

	return cloned, nil
}

func clonePromptRequest(request *PromptRequest) *PromptRequest {
	cloned := *request

	cloned.Images = make([]ImageAttachment, len(request.Images))
	for index := range request.Images {
		cloned.Images[index] = request.Images[index]
		cloned.Images[index].Data = bytes.Clone(request.Images[index].Data)
	}

	return &cloned
}

func (runtime *Runtime) validatePromptRequest(request *PromptRequest) error {
	if strings.TrimSpace(request.Text) == "" && len(request.Images) == 0 {
		return oops.In("assistant").Code("empty_prompt").Errorf("prompt text and images are empty")
	}

	if len(request.Images) == 0 {
		return nil
	}

	if strings.HasPrefix(strings.TrimSpace(request.Text), slashPrefix) {
		return oops.In("assistant").Code("slash_command_images").
			Errorf("slash commands do not accept image attachments")
	}

	if err := validatePromptImages(request.Images); err != nil {
		return err
	}

	if runtime.models == nil {
		return oops.In("assistant").Code("models_unavailable").Errorf("model registry is not configured")
	}

	selected, err := runtime.selectedModel()
	if err != nil {
		return err
	}

	return validateSelectedModelImageInput(&selected, request.Images)
}

func validatePromptImageAllocationBounds(images []ImageAttachment) error {
	if len(images) > maxPromptImages {
		return promptImageError(
			"image_count_limit", "prompt has %d images; maximum is %d", len(images), maxPromptImages,
		)
	}

	total := 0

	for index := range images {
		if len(images[index].Name) > maxPromptImageName {
			return promptImageError(
				"image_name_limit", "image %d name exceeds %d bytes", index+1, maxPromptImageName,
			)
		}

		if len(images[index].Data) > maxPromptImageBytes {
			return promptImageError("image_size_limit", "image %d exceeds the 5 MiB limit", index+1)
		}

		total += len(images[index].Data)
		if total > maxPromptImageTotal {
			return promptImageError("image_total_size_limit", "image attachments exceed the 20 MiB limit")
		}
	}

	return nil
}

func validatePromptImages(images []ImageAttachment) error {
	if err := validatePromptImageAllocationBounds(images); err != nil {
		return err
	}

	for index := range images {
		if err := validatePromptImage(&images[index], index); err != nil {
			return err
		}
	}

	return nil
}

func validatePromptImage(attachment *ImageAttachment, index int) error {
	if len(attachment.Data) == 0 {
		return promptImageError("invalid_image", "image %d is empty", index+1)
	}

	if len(attachment.Data) > maxPromptImageBytes {
		return promptImageError("image_size_limit", "image %d exceeds the 5 MiB limit", index+1)
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(attachment.Data))
	if err != nil {
		return oops.In("assistant").Code("invalid_image").
			With("image_index", index).Wrapf(err, "decode image %d", index+1)
	}

	return validatePromptImageMetadata(attachment, config, format, index)
}

func validatePromptImageMetadata(attachment *ImageAttachment, config image.Config, format string, index int) error {
	mimeType := imageMIMEType(format)
	if mimeType == "" || attachment.MIMEType != mimeType {
		return promptImageError(
			"invalid_image_mime", "image %d MIME type %q does not match format %q",
			index+1, attachment.MIMEType, format,
		)
	}

	if config.Width <= 0 || config.Height <= 0 || config.Width > maxPromptImagePixels/config.Height {
		return promptImageError(
			"image_dimensions_limit", "image %d dimensions must be positive and at most 40 megapixels", index+1,
		)
	}

	if attachment.Width != config.Width || attachment.Height != config.Height {
		return promptImageError("invalid_image_dimensions", "image %d dimensions do not match its data", index+1)
	}

	return nil
}

func imageMIMEType(format string) string {
	switch format {
	case "gif":
		return "image/gif"
	case "jpeg":
		return "image/jpeg"
	case "png":
		return imageMIMEPNG
	case "webp":
		return imageMIMEWebP
	default:
		return ""
	}
}

func promptImageError(code, format string, args ...any) error {
	return oops.In("assistant").Code(code).Errorf(format, args...)
}

func imageMetadata(attachment ImageAttachment) map[string]any {
	return map[string]any{
		executeNameKey: attachment.Name, "mime_type": attachment.MIMEType,
		"width": attachment.Width, "height": attachment.Height, "size": len(attachment.Data),
	}
}

func validateSelectedModelImageInput(selected *model.Model, images []ImageAttachment) error {
	if len(images) == 0 {
		return nil
	}

	return validateSelectedModelHasImageInput(selected, "prompt")
}

func validateSelectedModelHasImageInput(selected *model.Model, source string) error {
	if slices.Contains(selected.Input, model.InputImage) {
		return nil
	}

	return oops.In("assistant").Code("image_input_unsupported").
		With("image_source", source).
		With("provider", selected.Provider).
		With("model", selected.ID).
		Errorf("selected model %s/%s does not support image input", selected.Provider, selected.ID)
}

func messagesContainImages(messages []database.MessageEntity) bool {
	for index := range messages {
		if messageContainsImages(&messages[index]) {
			return true
		}
	}

	return false
}

func messageContainsImages(message *database.MessageEntity) bool {
	for index := range message.Parts {
		if message.Parts[index].Type == database.MessagePartImage {
			return true
		}
	}

	return false
}

func validateModelContextImageInput(selected *model.Model, messages []database.MessageEntity) error {
	if !messagesContainImages(messages) || slices.Contains(selected.Input, model.InputImage) {
		return nil
	}

	return validateSelectedModelHasImageInput(selected, "conversation_history")
}

func imagePartMetadata(name string, width, height int) map[string]any {
	return map[string]any{executeNameKey: name, "width": width, "height": height}
}
