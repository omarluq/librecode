package core_test

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/core"
)

func TestSkillsCacheReturnsSameResultAsLoadSkills(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestFile(t, filepath.Join(
		cwd,
		core.ConfigDirName,
		"skills",
		"alpha",
		"SKILL.md",
	), skillMarkdown("alpha"))

	direct := core.LoadSkills(cwd, nil, true)

	cache := core.NewSkillsCache()
	t.Cleanup(cache.Close)

	cached := cache.Get(cwd)

	require.Len(t, cached.Skills, len(direct.Skills))
	assert.Equal(t, direct.Skills[0].Name, cached.Skills[0].Name)
	assert.Equal(t, direct.Skills[0].FilePath, cached.Skills[0].FilePath)
}

func TestSkillsCacheReturnsCachedResultOnSecondCall(t *testing.T) {
	cwd := newOutsideTempDir(t)
	home := newOutsideTempDir(t)
	t.Setenv("HOME", home)

	skillPath := filepath.Join(cwd, core.ConfigDirName, "skills", "alpha", "SKILL.md")
	writeTestFile(t, skillPath, skillMarkdown("alpha"))

	cache := core.NewSkillsCache()
	t.Cleanup(cache.Close)

	first := cache.Get(cwd)
	require.Len(t, first.Skills, 1)

	// Overwrite the skill with a different name. The cached result should still
	// have the original name because the loader has not been re-invoked yet.
	writeTestFile(t, skillPath, skillMarkdown("alpha-renamed"))

	// Give fsnotify no chance to process: call immediately.
	second := cache.Get(cwd)
	require.Len(t, second.Skills, 1)
	assert.Equal(t, first.Skills[0].Name, second.Skills[0].Name,
		"cache should serve the original result before invalidation")
}

func TestSkillsCacheInvalidatesOnFileChange(t *testing.T) {
	cwd := newOutsideTempDir(t)
	home := newOutsideTempDir(t)
	t.Setenv("HOME", home)

	skillPath := filepath.Join(cwd, core.ConfigDirName, "skills", "alpha", "SKILL.md")
	writeTestFile(t, skillPath, skillMarkdown("alpha"))

	cache := core.NewSkillsCache()
	t.Cleanup(cache.Close)

	first := cache.Get(cwd)
	require.Len(t, first.Skills, 1)
	assert.Equal(t, "alpha", first.Skills[0].Name)

	// Add a new skill and write to the existing skill file to trigger fsnotify.
	newSkillPath := filepath.Join(cwd, core.ConfigDirName, "skills", "beta", "SKILL.md")
	writeTestFile(t, newSkillPath, skillMarkdown("beta"))

	writeTestFile(t, skillPath, skillMarkdown("alpha-updated"))

	require.Eventually(t, func() bool {
		refreshed := cache.Get(cwd)

		return len(refreshed.Skills) == 2
	}, 3*time.Second, 50*time.Millisecond, "cache should invalidate after file change")
}

func TestSkillsCacheDebouncesBurstEvents(t *testing.T) {
	cwd := newOutsideTempDir(t)
	home := newOutsideTempDir(t)
	t.Setenv("HOME", home)

	skillPath := filepath.Join(cwd, core.ConfigDirName, "skills", "alpha", "SKILL.md")
	writeTestFile(t, skillPath, skillMarkdown("alpha"))

	cache := core.NewSkillsCache()
	t.Cleanup(cache.Close)

	first := cache.Get(cwd)
	require.Len(t, first.Skills, 1)
	assert.Equal(t, "alpha", first.Skills[0].Name)

	// Simulate a burst of writes to the same file (e.g. editor save + git checkout).
	for i := range 10 {
		writeTestFile(t, skillPath, skillMarkdown("burst-"+strconv.Itoa(i)))
	}

	// After the debounce window, the cache should reflect the final write.
	require.Eventually(t, func() bool {
		refreshed := cache.Get(cwd)

		return len(refreshed.Skills) == 1 && refreshed.Skills[0].Name == "burst-9"
	}, 3*time.Second, 50*time.Millisecond, "cache should reflect final write after debounce")
}

func TestSkillsCacheIsolatesByCWD(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cwdA := t.TempDir()
	cwdB := t.TempDir()

	writeTestFile(t, filepath.Join(cwdA, core.ConfigDirName, "skills", "a", "SKILL.md"), skillMarkdown("a"))
	writeTestFile(t, filepath.Join(cwdB, core.ConfigDirName, "skills", "b", "SKILL.md"), skillMarkdown("b"))

	cache := core.NewSkillsCache()
	t.Cleanup(cache.Close)

	resultA := cache.Get(cwdA)
	resultB := cache.Get(cwdB)

	require.Len(t, resultA.Skills, 1)
	assert.Equal(t, "a", resultA.Skills[0].Name)

	require.Len(t, resultB.Skills, 1)
	assert.Equal(t, "b", resultB.Skills[0].Name)
}

func TestSkillsCacheCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	cache := core.NewSkillsCache()

	cache.Close()
	cache.Close()

	assert.NotPanics(t, func() {
		cache.Close()
	})
}

func TestSkillsCacheReturnsEmptyForCwdWithNoSkills(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	cache := core.NewSkillsCache()
	t.Cleanup(cache.Close)

	result := cache.Get(cwd)

	assert.Empty(t, result.Skills)
}

func TestSkillsCacheInvalidatesWhenSkillRootCreatedLater(t *testing.T) {
	cwd := newOutsideTempDir(t)
	home := newOutsideTempDir(t)
	t.Setenv("HOME", home)

	// First Get with no skill directories — loads empty and watches ancestor.
	cache := core.NewSkillsCache()
	t.Cleanup(cache.Close)

	first := cache.Get(cwd)
	assert.Empty(t, first.Skills)

	// Create the skill root and a skill file after the initial load.
	skillPath := filepath.Join(cwd, core.ConfigDirName, "skills", "alpha", "SKILL.md")
	writeTestFile(t, skillPath, skillMarkdown("alpha"))

	// The ancestor watch should fire and invalidate the cache.
	require.Eventually(t, func() bool {
		refreshed := cache.Get(cwd)

		return len(refreshed.Skills) == 1 && refreshed.Skills[0].Name == "alpha"
	}, 3*time.Second, 50*time.Millisecond, "cache should invalidate when skill root is created")
}

func TestSkillsCacheGetIsSafeAfterClose(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestFile(t, filepath.Join(
		cwd,
		core.ConfigDirName,
		"skills",
		"alpha",
		"SKILL.md",
	), strings.Join([]string{
		frontmatterDelimiter,
		"name: alpha",
		"description: Alpha skill",
		frontmatterDelimiter,
		"Body.",
	}, "\n"))

	cache := core.NewSkillsCache()

	first := cache.Get(cwd)
	require.Len(t, first.Skills, 1)

	cache.Close()

	// After Close, Get should still work — it just does a direct load.
	second := cache.Get(cwd)
	assert.Len(t, second.Skills, 1)
}
