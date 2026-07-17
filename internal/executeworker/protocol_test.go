package executeworker_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/omarluq/librecode/internal/executeworker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtocolRejectsOversizedMessagesOnReadAndWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		write func(*bytes.Buffer) error
		name  string
		want  string
	}{
		{
			name: "result write",
			write: func(buffer *bytes.Buffer) error {
				return executeworker.Write(buffer, protocolMessage(
					"result", []byte(`"`+strings.Repeat("x", executeworker.MaxResultSize)+`"`),
				))
			},
			want: "result size",
		},
		{
			name: "frame write",
			write: func(buffer *bytes.Buffer) error {
				return executeworker.Write(buffer, protocolMessage(
					"rpc_result", []byte(`"`+strings.Repeat("x", executeworker.MaxFrameSize)+`"`),
				))
			},
			want: "frame size",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := test.write(new(bytes.Buffer))
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.want)
		})
	}

	var input bytes.Buffer
	require.NoError(t, binary.Write(&input, binary.BigEndian, uint32(executeworker.MaxFrameSize+1)))
	_, err := executeworker.Read(&input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "frame size")
}

func TestProtocolResponseBudgetsCompose(t *testing.T) {
	t.Parallel()

	message := protocolMessage("result", []byte(`"`+strings.Repeat("v", executeworker.MaxResultSize-2)+`"`))
	message.Stdout = strings.Repeat("\\", (executeworker.MaxFrameSize-executeworker.MaxResultSize)/(2*6))
	message.Stderr = message.Stdout

	var buffer bytes.Buffer
	require.NoError(t, executeworker.Write(&buffer, message))
	got, err := executeworker.Read(&buffer)
	require.NoError(t, err)
	assert.Equal(t, message.Type, got.Type)
	assert.Equal(t, message.Value, got.Value)
	assert.Equal(t, message.Stdout, got.Stdout)
	assert.Equal(t, message.Stderr, got.Stderr)
}

func protocolMessage(messageType string, value []byte) *executeworker.Message {
	return &executeworker.Message{
		Stderr: "", Source: "", Method: "", Mode: "", Name: "", Query: "", Stdout: "", Type: messageType,
		Error: "", ErrorKind: "", ValueKind: "", Input: nil, Value: value, Arguments: nil,
		ID: 0, ExitCode: 0,
	}
}
