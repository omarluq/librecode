package fswalk

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/charlievieth/fastwalk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalkSkipsAllWithoutError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o600))

	var visited atomic.Int64

	err := Walk(root, func(_ string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		visited.Add(1)

		return fs.SkipAll
	})

	require.NoError(t, err)
	assert.Equal(t, int64(1), visited.Load())
}

func TestWalkSkipOnFileSkipsRemainingSiblingFiles(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		skipErr error
		name    string
	}{
		{name: "fs_skip_dir", skipErr: fs.SkipDir},
		{name: "fastwalk_skip_files", skipErr: fastwalk.ErrSkipFiles},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			testWalkSkipDirOnFile(t, testCase.skipErr)
		})
	}
}

func testWalkSkipDirOnFile(t *testing.T, skipErr error) {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "b.txt"), []byte("b"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(root, "dir"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "dir", "c.txt"), []byte("c"), 0o600))

	visited := []string{}

	var visitedLock sync.Mutex

	appendVisited := func(path string) {
		visitedLock.Lock()
		defer visitedLock.Unlock()

		visited = append(visited, path)
	}

	err := Walk(root, func(currentPath string, dirEntry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relativePath, relErr := filepath.Rel(root, currentPath)
		if relErr != nil {
			return wrapFastwalkErr(relErr)
		}

		appendVisited(filepath.ToSlash(relativePath))

		if dirEntry.Name() == "a.txt" {
			return skipErr
		}

		return nil
	})

	require.NoError(t, err)
	visitedLock.Lock()
	defer visitedLock.Unlock()

	sort.Strings(visited)
	assert.Equal(t, []string{".", "a.txt", "dir", "dir/c.txt"}, visited)
}
