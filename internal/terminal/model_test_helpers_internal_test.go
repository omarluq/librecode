package terminal

import "github.com/omarluq/librecode/internal/model"

func disabledModelDiscovery() model.DiscoveryOptions {
	return model.DiscoveryOptions{
		Client:       nil,
		CachePath:    "",
		SourceURL:    "",
		CacheTTL:     0,
		FetchTimeout: 0,
		Enabled:      false,
	}
}

func promptDrafts(texts ...string) []promptDraft {
	drafts := make([]promptDraft, len(texts))
	for index, text := range texts {
		drafts[index] = promptDraft{Text: text, Images: nil}
	}

	return drafts
}

func promptDraftTexts(drafts []promptDraft) []string {
	texts := make([]string, len(drafts))
	for index := range drafts {
		texts[index] = drafts[index].Text
	}

	return texts
}
