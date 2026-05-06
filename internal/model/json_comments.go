package model

import "strings"

type jsonStripState struct {
	builder  strings.Builder
	inString bool
	escaped  bool
}

func stripJSONComments(input string) string {
	withoutComments := stripLineComments(input)

	return stripTrailingCommas(withoutComments)
}

func stripLineComments(input string) string {
	state := jsonStripState{builder: strings.Builder{}, inString: false, escaped: false}
	for index := 0; index < len(input); index++ {
		current := input[index]
		if state.inString {
			state.writeStringByte(current)
			continue
		}
		switch {
		case current == '"':
			state.beginString(current)
		case startsLineComment(input, index):
			index = skipUntilNewline(input, index+2, &state.builder)
		default:
			state.builder.WriteByte(current)
		}
	}

	return state.builder.String()
}

func stripTrailingCommas(input string) string {
	state := jsonStripState{builder: strings.Builder{}, inString: false, escaped: false}
	for index := 0; index < len(input); index++ {
		current := input[index]
		if state.inString {
			state.writeStringByte(current)
			continue
		}
		switch {
		case current == '"':
			state.beginString(current)
		case current == ',' && commaIsTrailing(input, index+1):
			continue
		default:
			state.builder.WriteByte(current)
		}
	}

	return state.builder.String()
}

func (state *jsonStripState) beginString(current byte) {
	state.inString = true
	state.builder.WriteByte(current)
}

func (state *jsonStripState) writeStringByte(current byte) {
	state.builder.WriteByte(current)
	if state.escaped {
		state.escaped = false
		return
	}
	if current == '\\' {
		state.escaped = true
		return
	}
	if current == '"' {
		state.inString = false
	}
}

func startsLineComment(input string, index int) bool {
	return input[index] == '/' && index+1 < len(input) && input[index+1] == '/'
}

func skipUntilNewline(input string, index int, builder *strings.Builder) int {
	for index < len(input) {
		if input[index] == '\n' {
			builder.WriteByte('\n')
			return index
		}
		index++
	}

	return len(input)
}

func commaIsTrailing(input string, start int) bool {
	for index := start; index < len(input); index++ {
		switch input[index] {
		case ' ', '\t', '\r', '\n':
			continue
		case '}', ']':
			return true
		default:
			return false
		}
	}

	return false
}
