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
		return luaSchemaTableJSON(typedValue)
	default:
		return []byte("null"), nil
	}
}

func luaSchemaTableJSON(table *lua.LTable) ([]byte, error) {
	if luaTableIsArray(table) {
		return luaSchemaArrayJSON(table)
	}

	return luaSchemaObjectJSON(table)
}

func luaTableIsArray(table *lua.LTable) bool {
	length := table.Len()
	if length == 0 {
		return false
	}

	isArray := true

	table.ForEach(func(key, _ lua.LValue) {
		if _, ok := key.(lua.LNumber); !ok {
			isArray = false
		}
	})

	return isArray
}

func luaSchemaArrayJSON(table *lua.LTable) ([]byte, error) {
	encodedArray := []byte{'['}

	for index := 1; index <= table.Len(); index++ {
		if index > 1 {
			encodedArray = append(encodedArray, ',')
		}

		encoded, err := luaSchemaJSON(table.RawGetInt(index))
		if err != nil {
			return nil, err
		}

		encodedArray = append(encodedArray, encoded...)
	}

	encodedArray = append(encodedArray, ']')

	return encodedArray, nil
}

func luaSchemaObjectJSON(table *lua.LTable) ([]byte, error) {
	encodedObject := []byte{'{'}

	first := true

	var encodeErr error

	table.ForEach(func(key, value lua.LValue) {
		if encodeErr != nil {
			return
		}

		encoded, err := luaSchemaJSON(value)
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
