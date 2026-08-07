package tool

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileMutationLocksReserveSamePathInRegistrationOrder(t *testing.T) {
	t.Parallel()

	locks := newFileMutationLocks()
	path := filepath.Join(t.TempDir(), "file.txt")
	first := locks.reserve(path)
	second := locks.reserve(path)
	secondStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	done := make(chan struct{})

	go func() {
		_, err := first.execute(t.Context(), func() (Result, error) {
			close(secondStarted)
			<-firstRelease

			return emptyToolResult(), nil
		})
		assert.NoError(t, err)
	}()

	<-secondStarted

	secondRan := make(chan struct{})

	go func() {
		_, err := second.execute(t.Context(), func() (Result, error) {
			close(secondRan)

			return emptyToolResult(), nil
		})
		assert.NoError(t, err)
		close(done)
	}()

	select {
	case <-secondRan:
		t.Fatal("second mutation ran before the first completed")
	default:
	}

	close(firstRelease)
	<-done
}

func TestFileMutationLocksCanceledWaitReleasesReservation(t *testing.T) {
	t.Parallel()

	locks := newFileMutationLocks()
	path := filepath.Join(t.TempDir(), "file.txt")
	first := locks.reserve(path)
	second := locks.reserve(path)
	third := locks.reserve(path)
	firstRelease := make(chan struct{})
	firstStarted := make(chan struct{})
	firstDone := make(chan error, 1)

	go func() {
		_, err := first.execute(t.Context(), func() (Result, error) {
			close(firstStarted)
			<-firstRelease

			return emptyToolResult(), nil
		})
		firstDone <- err
	}()

	<-firstStarted

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := second.execute(ctx, func() (Result, error) {
		t.Fatal("canceled mutation executed")

		return emptyToolResult(), nil
	})
	require.ErrorIs(t, err, context.Canceled)

	close(firstRelease)
	require.NoError(t, <-firstDone)

	_, err = third.execute(t.Context(), func() (Result, error) { return emptyToolResult(), nil })
	require.NoError(t, err)
}

func TestFileMutationLocksCanceledReservationDoesNotWaitForPrevious(t *testing.T) {
	t.Parallel()

	locks := newFileMutationLocks()
	path := filepath.Join(t.TempDir(), "file.txt")
	first := locks.reserve(path)
	second := locks.reserve(path)
	firstRelease := make(chan struct{})
	firstStarted := make(chan struct{})
	firstDone := make(chan error, 1)

	go func() {
		_, err := first.execute(t.Context(), func() (Result, error) {
			close(firstStarted)
			<-firstRelease

			return emptyToolResult(), nil
		})
		firstDone <- err
	}()

	<-firstStarted

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	done := make(chan error, 1)
	mutated := make(chan struct{}, 1)

	go func() {
		_, err := second.execute(ctx, func() (Result, error) {
			mutated <- struct{}{}

			return emptyToolResult(), nil
		})
		done <- err
	}()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("canceled reservation waited for the preceding mutation")
	}

	select {
	case <-mutated:
		t.Fatal("canceled mutation executed")
	default:
	}

	close(firstRelease)
	require.NoError(t, <-firstDone)
}

func TestPreparedCallReleaseRemovesUnexecutedReservation(t *testing.T) {
	t.Parallel()

	locks := newFileMutationLocks()
	path := filepath.Join(t.TempDir(), "file.txt")
	first := locks.reserve(path)
	second := locks.reserve(path)

	first.remove()

	_, err := second.execute(t.Context(), func() (Result, error) {
		return emptyToolResult(), nil
	})
	require.NoError(t, err)
}

func TestRegistryPreparesEditAndWriteInSourceOrderForSamePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := fileToolsTestFile
	absolutePath := filepath.Join(root, path)
	initial := "before"
	require.NoError(t, os.WriteFile(absolutePath, []byte(initial), 0o600))

	registry := NewRegistry(root)
	editArguments, err := ArgumentsFromRaw([]byte(
		`{"path":"file.txt","edits":[{"old_text":"before","new_text":"edited"}]}`,
	))
	require.NoError(t, err)

	writeArguments, err := ArgumentsFromRaw([]byte(`{"path":"file.txt","content":"written"}`))
	require.NoError(t, err)

	editCall, err := registry.Prepare("edit", editArguments)
	require.NoError(t, err)

	writeCall, err := registry.Prepare("write", writeArguments)
	require.NoError(t, err)

	editCall.Admit()
	writeCall.Admit()

	done := make(chan error, 2)

	go func() {
		_, executeErr := (&writeCall).Execute(t.Context())
		done <- executeErr
	}()
	go func() {
		_, executeErr := (&editCall).Execute(t.Context())
		done <- executeErr
	}()

	require.NoError(t, <-done)
	require.NoError(t, <-done)

	content, err := readResolvedPath(absolutePath)
	require.NoError(t, err)
	assert.Equal(t, "written", string(content))
}

func TestFileMutationLocksCanonicalizeSymlinkPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	realDirectory := filepath.Join(root, "real")
	require.NoError(t, os.Mkdir(realDirectory, 0o750))

	linkDirectory := filepath.Join(root, "link")
	require.NoError(t, os.Symlink(realDirectory, linkDirectory))

	tests := []struct {
		name     string
		realPath string
		linkPath string
	}{
		{
			name:     "existing parent",
			realPath: filepath.Join(realDirectory, "file.txt"),
			linkPath: filepath.Join(linkDirectory, "file.txt"),
		},
		{
			name:     "multiple missing components",
			realPath: filepath.Join(realDirectory, "missing", "nested", "file.txt"),
			linkPath: filepath.Join(linkDirectory, "missing", "nested", "file.txt"),
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, canonicalMutationPath(testCase.realPath), canonicalMutationPath(testCase.linkPath))
		})
	}
}
