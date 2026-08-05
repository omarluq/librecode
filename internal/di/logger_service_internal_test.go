package di

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoggerWriter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want        io.Writer
		name        string
		interactive bool
	}{
		{name: "interactive discards terminal output", interactive: true, want: io.Discard},
		{name: "noninteractive writes to stdout", interactive: false, want: os.Stdout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			service := &ConfigService{cfg: nil, path: "", interactive: test.interactive}
			assert.Equal(t, test.want, loggerWriter(service))
		})
	}
}
