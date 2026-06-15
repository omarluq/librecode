package provider

import "errors"

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("boom") }
