package browser

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testURL = "https://example.com"

func TestOpen(t *testing.T) {
	t.Parallel()

	openErr := errors.New("open failed")

	tests := []struct {
		openErr error
		wantErr error
		name    string
		target  string
	}{
		{
			openErr: nil,
			wantErr: nil,
			name:    "opens url",
			target:  testURL,
		},
		{
			openErr: openErr,
			wantErr: openErr,
			name:    "wraps opener errors",
			target:  testURL,
		},
		{
			openErr: nil,
			wantErr: nil,
			name:    "passes empty url to opener",
			target:  "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			opener := &recordingOpener{
				err:  test.openErr,
				urls: nil,
			}

			err := open(test.target, opener.openURL)

			if test.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, test.wantErr)
			}

			assert.Equal(t, []string{test.target}, opener.urls)
		})
	}
}

type recordingOpener struct {
	err  error
	urls []string
}

func (r *recordingOpener) openURL(url string) error {
	r.urls = append(r.urls, url)

	return r.err
}
