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
	return statResolvedPath(path)
}
