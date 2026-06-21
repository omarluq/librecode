package extension

import (
	"math"
	"strconv"

	"github.com/samber/oops"
	lua "github.com/yuin/gopher-lua"
)

func luaSchemaRaw(table *lua.LTable) ([]byte, error) {
	if table == nil {
		return nil, nil
	}

	encoded, err := luaSchemaJSON(table)
	if err != nil {
		return nil, err
	}

	return encoded, nil
}

func luaSchemaJSON(value lua.LValue) ([]byte, error) {
	return luaSchemaJSONWithSeen(value, map[*lua.LTable]struct{}{})
}

func luaSchemaJSONWithSeen(value lua.LValue, seen map[*lua.LTable]struct{}) ([]byte, error) {
	switch typedValue := value.(type) {
	case lua.LBool:
		if bool(typedValue) {
			return []byte("true"), nil
		}

		return []byte("false"), nil
	case lua.LNumber:
		floatValue := float64(typedValue)
		if math.IsNaN(floatValue) || math.IsInf(floatValue, 0) {
			return nil, oops.In("extension").
				Code("invalid_tool_schema").
				Errorf("tool schema contains a non-finite number")
		}

		return []byte(strconv.FormatFloat(floatValue, 'f', -1, 64)), nil
	case lua.LString:
		return []byte(strconv.Quote(string(typedValue))), nil
	case *lua.LTable:
		if _, ok := seen[typedValue]; ok {
			return nil, oops.In("extension").
				Code("invalid_tool_schema").
				Errorf("tool schema contains cyclic table reference")
		}

		seen[typedValue] = struct{}{}
		encoded, err := luaSchemaTableJSON(typedValue, seen)
		delete(seen, typedValue)

		return encoded, err
	default:
		return []byte("null"), nil
	}
}

func luaSchemaTableJSON(table *lua.LTable, seen map[*lua.LTable]struct{}) ([]byte, error) {
	if luaTableIsArray(table) {
		return luaSchemaArrayJSON(table, seen)
	}

	return luaSchemaObjectJSON(table, seen)
}

func luaTableIsArray(table *lua.LTable) bool {
	length := table.Len()
	if length == 0 {
		return false
	}

	isArray := true
	keyCount := 0

	table.ForEach(func(key, _ lua.LValue) {
		number, ok := key.(lua.LNumber)
		if !ok {
			isArray = false

			return
		}

		floatIndex := float64(number)
		intIndex := int(floatIndex)

		if float64(intIndex) != floatIndex || intIndex < 1 || intIndex > length {
			isArray = false

			return
		}

		keyCount++
	})

	return isArray && keyCount == length
}

func luaSchemaArrayJSON(table *lua.LTable, seen map[*lua.LTable]struct{}) ([]byte, error) {
	encodedArray := []byte{'['}

	for index := 1; index <= table.Len(); index++ {
		if index > 1 {
			encodedArray = append(encodedArray, ',')
		}

		encoded, err := luaSchemaJSONWithSeen(table.RawGetInt(index), seen)
		if err != nil {
			return nil, err
		}

		encodedArray = append(encodedArray, encoded...)
	}

	encodedArray = append(encodedArray, ']')

	return encodedArray, nil
}

func luaSchemaObjectJSON(table *lua.LTable, seen map[*lua.LTable]struct{}) ([]byte, error) {
	encodedObject := []byte{'{'}

	first := true

	var encodeErr error

	table.ForEach(func(key, value lua.LValue) {
		if encodeErr != nil {
			return
		}

		encoded, err := luaSchemaJSONWithSeen(value, seen)
		if err != nil {
			encodeErr = err

			return
		}

		if !first {
			encodedObject = append(encodedObject, ',')
		}

		first = false

		encodedObject = append(encodedObject, strconv.Quote(key.String())...)
		encodedObject = append(encodedObject, ':')
		encodedObject = append(encodedObject, encoded...)
	})

	if encodeErr != nil {
		return nil, encodeErr
	}

	encodedObject = append(encodedObject, '}')

	return encodedObject, nil
}
