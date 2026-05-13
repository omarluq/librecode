package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

const defaultLocalExtensionSource = "path:.librecode/extensions"

func unmarshalConfig(viperInstance *viper.Viper, cfg *Config) error {
	settings := viperInstance.AllSettings()
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: decodeExtensionUseHook,
		Result:     cfg,
		TagName:    "mapstructure",
	})
	if err != nil {
		return fmt.Errorf("config: create decoder: %w", err)
	}
	if err := decoder.Decode(settings); err != nil {
		return fmt.Errorf("config: unmarshal: %w", err)
	}

	return nil
}

func decodeExtensionUseHook(_, to reflect.Type, data any) (any, error) {
	if to != reflect.TypeFor[ExtensionUse]() {
		return data, nil
	}

	switch value := data.(type) {
	case string:
		return ExtensionUse{Source: strings.TrimSpace(value), Version: ""}, nil
	case map[string]any:
		return decodeExtensionUseMap(value), nil
	case map[any]any:
		converted := make(map[string]any, len(value))
		for key, mapValue := range value {
			converted[fmt.Sprint(key)] = mapValue
		}
		return decodeExtensionUseMap(converted), nil
	default:
		return data, nil
	}
}

func decodeExtensionUseMap(value map[string]any) ExtensionUse {
	return ExtensionUse{
		Source:  extensionUseString(value["source"]),
		Version: extensionUseString(value["version"]),
	}
}

func extensionUseString(value any) string {
	if value == nil {
		return ""
	}

	return strings.TrimSpace(fmt.Sprint(value))
}
