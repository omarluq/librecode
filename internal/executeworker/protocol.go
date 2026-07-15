// Package executeworker implements the bounded framed protocol used to isolate
// provider-facing MVM execution in a helper process.
package executeworker

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/omarluq/librecode/internal/tool"
)

const (
	// MaxFrameSize is the largest encoded protocol message accepted by a worker.
	MaxFrameSize = 2 << 20
	// MaxResultSize is the largest encoded evaluation value accepted across the protocol.
	MaxResultSize = 1 << 20
)

// ToolCallResult is the typed result of a tools.Call callback. Keeping this
// envelope typed on both sides of the worker boundary preserves content blocks
// and details instead of decoding them through map[string]any.
type ToolCallResult struct {
	Details map[string]any      `json:"details"`
	Error   string              `json:"error,omitempty"`
	Content []tool.ContentBlock `json:"content"`
	IsError bool                `json:"is_error"`
}

// Message is a framed request or response exchanged with an execute worker.
type Message struct {
	Stderr      string          `json:"stderr,omitempty"`
	Source      string          `json:"source,omitempty"`
	Method      string          `json:"method,omitempty"`
	Mode        string          `json:"mode,omitempty"`
	Name        string          `json:"name,omitempty"`
	Query       string          `json:"query,omitempty"`
	Stdout      string          `json:"stdout,omitempty"`
	Type        string          `json:"type"`
	Error       string          `json:"error,omitempty"`
	ErrorKind   string          `json:"error_kind,omitempty"`
	ValueKind   string          `json:"value_kind,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
	Value       json.RawMessage `json:"value,omitempty"`
	Arguments   json.RawMessage `json:"arguments,omitempty"`
	ID          uint64          `json:"id,omitempty"`
	ExitCode    int             `json:"exit_code,omitempty"`
	SourceLimit int             `json:"source_limit,omitempty"`
	OutputLimit int             `json:"output_limit,omitempty"`
}

const (
	toolCallResultKind = "tool_call_result"
	jsonNullValue      = "null"
)

func newMessage(messageType string) Message {
	return Message{
		Stderr: "", Source: "", Method: "", Mode: "", Name: "", Query: "", Stdout: "",
		Type: messageType, Error: "", ErrorKind: "", ValueKind: "", Input: nil, Value: nil,
		Arguments: nil, ID: 0, ExitCode: 0, SourceLimit: 0, OutputLimit: 0,
	}
}

// Read decodes one length-prefixed message from r.
func Read(reader io.Reader) (Message, error) {
	var size [4]byte
	if _, err := io.ReadFull(reader, size[:]); err != nil {
		return Message{}, fmt.Errorf("read execute worker frame size: %w", err)
	}

	frameSize := binary.BigEndian.Uint32(size[:])
	if frameSize == 0 || frameSize > MaxFrameSize {
		return Message{}, fmt.Errorf("execute worker frame size %d exceeds limit %d", frameSize, MaxFrameSize)
	}

	payload := make([]byte, frameSize)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return Message{}, fmt.Errorf("read execute worker frame: %w", err)
	}

	var message Message
	if err := json.Unmarshal(payload, &message); err != nil {
		return Message{}, fmt.Errorf("decode execute worker frame: %w", err)
	}

	if err := validateMessage(&message); err != nil {
		return Message{}, err
	}

	return message, nil
}

// Write encodes one length-prefixed message to w.
func Write(writer io.Writer, message *Message) error {
	if err := validateMessage(message); err != nil {
		return err
	}

	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode execute worker frame: %w", err)
	}

	if len(payload) > MaxFrameSize {
		return fmt.Errorf("execute worker frame size %d exceeds limit %d", len(payload), MaxFrameSize)
	}

	if len(payload) > math.MaxUint32 {
		return fmt.Errorf("execute worker frame size %d exceeds uint32", len(payload))
	}

	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(payload)&math.MaxUint32))

	if err := writeFull(writer, size[:]); err != nil {
		return fmt.Errorf("write execute worker frame size: %w", err)
	}

	if err := writeFull(writer, payload); err != nil {
		return fmt.Errorf("write execute worker frame: %w", err)
	}

	return nil
}

func validateMessage(message *Message) error {
	if message == nil || message.Type == "" {
		return errors.New("invalid execute worker message: missing type")
	}

	if message.Type == "result" && len(message.Value) > MaxResultSize {
		return fmt.Errorf("execute worker result size %d exceeds limit %d", len(message.Value), MaxResultSize)
	}

	return nil
}

func writeFull(w io.Writer, payload []byte) error {
	for len(payload) > 0 {
		written, err := w.Write(payload)
		if err != nil {
			return fmt.Errorf("write bytes: %w", err)
		}

		if written <= 0 {
			return io.ErrShortWrite
		}

		payload = payload[written:]
	}

	return nil
}
