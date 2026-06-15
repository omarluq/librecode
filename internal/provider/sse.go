package provider

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/units"
)

const (
	sseInitialBufferSize = 64 * units.KiB
	sseMaxBufferSize     = 8 * units.MiB
)

var errSSEDone = errors.New("sse done")

type sseEvent struct {
	Name string
	Data string
}

func scanSSEEvents(reader io.Reader, handle func(sseEvent) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, sseInitialBufferSize), sseMaxBufferSize)

	eventName := ""
	dataLines := []string{}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := dispatchSSEEvent(eventName, dataLines, handle); err != nil {
				return err
			}

			eventName = ""
			dataLines = dataLines[:0]

			continue
		}

		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value, ok := strings.Cut(line, ":")
		if !ok {
			field = line
			value = ""
		} else {
			value = strings.TrimPrefix(value, " ")
		}

		switch field {
		case "event":
			eventName = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}

	if err := scanner.Err(); err != nil {
		return oops.In("provider").Code("sse_read").Wrapf(err, "read provider stream")
	}

	return dispatchSSEEvent(eventName, dataLines, handle)
}

func dispatchSSEEvent(name string, dataLines []string, handle func(sseEvent) error) error {
	if len(dataLines) == 0 {
		return nil
	}

	return handle(sseEvent{Name: name, Data: strings.Join(dataLines, "\n")})
}

func scanSSEDataLines(reader io.Reader, handle func(string) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, sseInitialBufferSize), sseMaxBufferSize)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		if err := handle(strings.TrimSpace(strings.TrimPrefix(line, "data:"))); err != nil {
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		return oops.In("provider").Code("sse_read").Wrapf(err, "read provider stream")
	}

	return nil
}

func decodeSSEJSON(data string, target any, code string) error {
	if err := json.Unmarshal([]byte(data), target); err != nil {
		return oops.In("provider").Code(code).Wrapf(err, "decode provider stream event")
	}

	return nil
}
