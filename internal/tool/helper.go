package tool

import "os"

const (
	detailTruncation     = "truncation"
	detailFullOutputPath = "fullOutputPath"
)

func emptyToolResult() Result {
	return Result{Details: map[string]any{}, Content: []ContentBlock{}}
}

func filepathStat(path string) (os.FileInfo, error) {
	//nolint:gosec // Tool paths are intentionally user/model-selected workspace paths.
	return os.Stat(path)
}
