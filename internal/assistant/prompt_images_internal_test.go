package assistant

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

const testWebPName = "screen.webp"

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	var output bytes.Buffer
	require.NoError(t, png.Encode(&output, image.NewRGBA(image.Rect(0, 0, width, height))))

	return output.Bytes()
}

func testWebP(t *testing.T) []byte {
	t.Helper()

	data, err := os.ReadFile("testdata/prompt.webp")
	require.NoError(t, err)

	return data
}

func TestValidatePromptImagesSupportsWebP(t *testing.T) {
	t.Parallel()

	attachment := ImageAttachment{
		Name: testWebPName, MIMEType: imageMIMEWebP, Data: testWebP(t), Width: 75, Height: 100,
	}
	require.NoError(t, validatePromptImages([]ImageAttachment{attachment}))

	attachment.MIMEType = imageMIMEPNG
	err := validatePromptImages([]ImageAttachment{attachment})
	require.Error(t, err)
	coded, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "invalid_image_mime", coded.Code())
}

func TestValidatePromptImagesAndCloneBoundary(t *testing.T) {
	t.Parallel()

	data := testPNG(t, 2, 3)
	images := []ImageAttachment{{Name: "screen.png", MIMEType: imageMIMEPNG, Data: data, Width: 2, Height: 3}}
	require.NoError(t, validatePromptImages(images))

	request := &PromptRequest{
		OnEvent: nil, OnRetry: nil, OnUserEntry: nil, OnSteeringReturn: nil, ParentEntryID: nil,
		SessionID: "", CWD: "", Text: "", Images: images, Name: "", ResumeLatest: false, HideUserPrompt: false,
	}
	cloned := clonePromptRequest(request)
	cloned.Images[0].Data[0]++
	assert.NotEqual(t, cloned.Images[0].Data[0], request.Images[0].Data[0])
}

func TestLifecyclePromptRequestAttachments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		request   *PromptRequest
		name      string
		wantName  string
		wantCount int
	}{
		{name: "nil request", request: nil, wantName: "", wantCount: 0},
		{
			name: "no images",
			request: &PromptRequest{
				OnEvent: nil, OnRetry: nil, OnUserEntry: nil, OnSteeringReturn: nil, ParentEntryID: nil,
				SessionID: "", CWD: "", Text: "", Images: nil, Name: "",
				ResumeLatest: false, HideUserPrompt: false,
			},
			wantName: "", wantCount: 0,
		},
		{
			name: "image metadata",
			request: &PromptRequest{
				OnEvent: nil, OnRetry: nil, OnUserEntry: nil, OnSteeringReturn: nil, ParentEntryID: nil,
				SessionID: "", CWD: "", Text: "", Name: "", ResumeLatest: false, HideUserPrompt: false,
				Images: []ImageAttachment{{
					Name: testWebPName, MIMEType: imageMIMEWebP, Data: []byte{1, 2}, Width: 3, Height: 4,
				}},
			},
			wantCount: 1, wantName: testWebPName,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			payload := lifecyclePromptRequest(testCase.request)
			assert.Len(t, payload.Attachments, testCase.wantCount)

			if testCase.wantCount > 0 {
				assert.Equal(t, testCase.wantName, payload.Attachments[0][executeNameKey])
				assert.Equal(t, 2, payload.Attachments[0]["size"])
			}
		})
	}
}

func TestValidateSelectedModelImageInput(t *testing.T) {
	t.Parallel()

	images := []ImageAttachment{{
		Name: "", MIMEType: imageMIMEPNG, Data: []byte{1}, Width: 1, Height: 1,
	}}
	vision := promptImageTestModel("vision", []model.InputMode{model.InputText, model.InputImage})
	require.NoError(t, validateSelectedModelImageInput(&vision, images))

	textOnly := promptImageTestModel("text-only", []model.InputMode{model.InputText})
	err := validateSelectedModelImageInput(&textOnly, images)
	require.Error(t, err)
	coded, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "image_input_unsupported", coded.Code())
}

func promptImageTestModel(id string, input []model.InputMode) model.Model {
	return model.Model{
		ThinkingLevelMap: nil, Headers: nil, Compat: nil, Provider: "image-test-provider", ID: id,
		Name: id, API: "", BaseURL: "", Input: input,
		Cost:          model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow: 0, MaxTokens: 0, Reasoning: false,
	}
}

func TestValidatePromptImagesRejectsUnsafeInput(t *testing.T) {
	t.Parallel()

	data := testPNG(t, 2, 3)

	tests := []ImageAttachment{
		{Name: "", MIMEType: "image/jpeg", Data: data, Width: 2, Height: 3},
		{Name: "", MIMEType: imageMIMEPNG, Data: data, Width: 3, Height: 2},
		{Name: "", MIMEType: imageMIMEPNG, Data: []byte("not an image"), Width: 1, Height: 1},
	}
	for _, attachment := range tests {
		require.Error(t, validatePromptImages([]ImageAttachment{attachment}))
	}

	many := make([]ImageAttachment, maxPromptImages+1)
	require.Error(t, validatePromptImages(many))

	longName := ImageAttachment{
		Name:     string(bytes.Repeat([]byte{'x'}, maxPromptImageName+1)),
		MIMEType: imageMIMEPNG, Data: data, Width: 2, Height: 3,
	}
	require.Error(t, validatePromptImages([]ImageAttachment{longName}))
}

func TestValidateModelContextImageInput(t *testing.T) {
	t.Parallel()

	messages := []database.MessageEntity{{
		Timestamp: time.Time{}, Role: database.RoleUser, Content: "", Provider: "", Model: "",
		Parts: []database.MessagePartEntity{{
			Text: "", MIMEType: imageMIMEPNG, Name: "", Type: database.MessagePartImage,
			Data: []byte{1}, Width: 1, Height: 1,
		}},
	}}
	vision := promptImageTestModel("vision", []model.InputMode{model.InputText, model.InputImage})
	require.NoError(t, validateModelContextImageInput(&vision, messages))

	textOnly := promptImageTestModel("text-only", []model.InputMode{model.InputText})
	err := validateModelContextImageInput(&textOnly, messages)
	require.Error(t, err)
	coded, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "image_input_unsupported", coded.Code())
}
