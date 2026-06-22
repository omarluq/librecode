// Package fswalk centralizes fast filesystem traversal defaults.
package fswalk

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/charlievieth/fastwalk"
)

// Walk walks root using fastwalk with repo-wide defaults for filesystem scans.
func Walk(root string, walkFn fs.WalkDirFunc) error {
	return walk(root, fastwalkConfig(), walkFn)
}

func fastwalkConfig() *fastwalk.Config {
	config := fastwalk.DefaultConfig.Copy()
	config.Sort = fastwalk.SortLexical
	config.ToSlash = false

	return config
}

func walk(root string, config *fastwalk.Config, walkFn fs.WalkDirFunc) error {
	err := fastwalk.Walk(config, root, adaptWalkFunc(walkFn))
	if errors.Is(err, fs.SkipAll) {
		return nil
	}

	return wrapFastwalkErr(err)
}

func wrapFastwalkErr(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("walk filesystem: %w", err)
}

func adaptWalkFunc(walkFn fs.WalkDirFunc) fs.WalkDirFunc {
	return func(currentPath string, dirEntry fs.DirEntry, walkErr error) error {
		err := walkFn(currentPath, dirEntry, walkErr)
		if errors.Is(err, fs.SkipDir) && dirEntry != nil && !dirEntry.IsDir() {
			return fastwalk.ErrSkipFiles
		}

		return err
	}
}
