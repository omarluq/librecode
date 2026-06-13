package model

func modelDecodeTokenUsageError(err error) error {
	return modelError(err, "decode token usage")
}
