package provider

import (
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/samber/oops"
	gosse "github.com/tmaxmax/go-sse"

	"github.com/omarluq/librecode/internal/units"
)

const sseMaxEventSize = 8 * units.MiB

var errSSEDone = errors.New("sse done")

type sseEvent struct {
	Name string
	Data string
}

func scanSSEEvents(reader io.Reader, handle func(sseEvent) error) error {
	config := &gosse.ReadConfig{MaxEventSize: sseMaxEventSize}
	for event, err := range gosse.Read(reader, config) {
		if err != nil {
			return oops.In("provider").Code("sse_read").Wrapf(err, "read provider stream")
		}

		if strings.TrimSpace(event.Data) == "" {
			continue
		}

		if err := handle(sseEvent{Name: event.Type, Data: event.Data}); err != nil {
			return err
		}
	}

	return nil
}

func scanSSEDataLines(reader io.Reader, handle func(string) error) error {
	return scanSSEEvents(reader, func(event sseEvent) error {
		for line := range strings.SplitSeq(event.Data, "\n") {
			data := strings.TrimSpace(line)
			if data == "" {
				continue
			}

			if err := handle(data); err != nil {
				return err
			}
		}

		return nil
	})
}

func decodeSSEJSON(data string, target any, code string) error {
	if err := json.Unmarshal([]byte(data), target); err != nil {
		return oops.In("provider").Code(code).Wrapf(err, "decode provider stream event")
	}

	return nil
}
