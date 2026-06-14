package core_test

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/omarluq/librecode/internal/core"
)

// --- helpers --------------------------------------------------------

func writeSkill(t *testing.T, cwd, name string) {
	t.Helper()

	writeTestFile(t, filepath.Join(cwd, core.ConfigDirName, "skills", name, "SKILL.md"), skillMarkdown(name))
}

func writeAlphaSkill(t *testing.T, cwd string) string {
	t.Helper()

	skillPath := filepath.Join(cwd, core.ConfigDirName, "skills", "alpha", "SKILL.md")
	writeTestFile(t, skillPath, skillMarkdown("alpha"))

	return skillPath
}

// --- suite ----------------------------------------------------------

type SkillsCacheSuite struct {
	suite.Suite
	cache     *core.SkillsCache
	cwd       string
	skillPath string
}

func TestSkillsCacheSuite(t *testing.T) {
	t.Setenv("HOME", newOutsideTempDir(t))
	suite.Run(t, new(SkillsCacheSuite))
}

func (s *SkillsCacheSuite) SetupTest() {
	s.cwd = newOutsideTempDir(s.T())
	s.cache = core.NewSkillsCache()
	s.T().Cleanup(s.cache.Close)
}

func (s *SkillsCacheSuite) writeAlpha() {
	s.skillPath = writeAlphaSkill(s.T(), s.cwd)
}

// --- basic Get ------------------------------------------------------

func (s *SkillsCacheSuite) TestReturnsEmptyForCWDWithNoSkills() {
	result := s.cache.Get(s.cwd)

	s.Empty(result.Skills)
}

func (s *SkillsCacheSuite) TestReturnsSkillFromCWD() {
	s.writeAlpha()

	result := s.cache.Get(s.cwd)

	s.Require().Len(result.Skills, 1)
	s.Equal("alpha", result.Skills[0].Name)
}

func (s *SkillsCacheSuite) TestServesCachedResultBeforeInvalidation() {
	s.writeAlpha()
	first := s.cache.Get(s.cwd)
	s.Require().Len(first.Skills, 1)

	// Overwrite with a different name; cache should still have the original.
	writeTestFile(s.T(), s.skillPath, skillMarkdown("alpha-renamed"))

	second := s.cache.Get(s.cwd)

	s.Require().Len(second.Skills, 1)
	s.Equal(first.Skills[0].Name, second.Skills[0].Name)
}

func (s *SkillsCacheSuite) TestParityWithDirectLoad() {
	s.writeAlpha()

	direct := core.LoadSkills(s.cwd, nil, true)
	cached := s.cache.Get(s.cwd)

	s.Require().Len(cached.Skills, len(direct.Skills))
	s.Equal(direct.Skills[0].Name, cached.Skills[0].Name)
	s.Equal(direct.Skills[0].FilePath, cached.Skills[0].FilePath)
}

// --- lifecycle ------------------------------------------------------

func (s *SkillsCacheSuite) TestCloseIsIdempotent() {
	s.cache.Close()
	s.cache.Close()

	s.NotPanics(func() { s.cache.Close() })
}

func (s *SkillsCacheSuite) TestGetIsSafeAfterClose() {
	s.writeAlpha()

	first := s.cache.Get(s.cwd)
	s.Require().Len(first.Skills, 1)

	s.cache.Close()

	// After Close, Get should still work — it just does a direct load.
	second := s.cache.Get(s.cwd)

	s.Len(second.Skills, 1)
}

// --- isolation ------------------------------------------------------

func (s *SkillsCacheSuite) TestCWDIsolation() {
	writeSkill(s.T(), s.cwd, "a")

	cwdB := newOutsideTempDir(s.T())
	writeSkill(s.T(), cwdB, "b")

	resultA := s.cache.Get(s.cwd)
	resultB := s.cache.Get(cwdB)

	s.Require().Len(resultA.Skills, 1)
	s.Equal("a", resultA.Skills[0].Name)

	s.Require().Len(resultB.Skills, 1)
	s.Equal("b", resultB.Skills[0].Name)
}

// --- fsnotify invalidation ------------------------------------------

func (s *SkillsCacheSuite) TestInvalidatesOnFileChange() {
	s.writeAlpha()

	first := s.cache.Get(s.cwd)
	s.Require().Len(first.Skills, 1)
	s.Equal("alpha", first.Skills[0].Name)

	// Add a new skill and touch the existing file to trigger fsnotify.
	writeSkill(s.T(), s.cwd, "beta")
	writeTestFile(s.T(), s.skillPath, skillMarkdown("alpha-updated"))

	s.Require().Eventually(func() bool {
		refreshed := s.cache.Get(s.cwd)

		return len(refreshed.Skills) == 2
	}, 3*time.Second, 50*time.Millisecond)
}

func (s *SkillsCacheSuite) TestInvalidatesWhenSkillRootCreatedLater() {
	first := s.cache.Get(s.cwd)

	s.Empty(first.Skills)

	// Create the skill root after the initial empty load.
	writeSkill(s.T(), s.cwd, "alpha")

	s.Require().Eventually(func() bool {
		refreshed := s.cache.Get(s.cwd)

		return len(refreshed.Skills) == 1 && refreshed.Skills[0].Name == "alpha"
	}, 3*time.Second, 50*time.Millisecond)
}

func (s *SkillsCacheSuite) TestDebouncesBurstEvents() {
	s.writeAlpha()

	first := s.cache.Get(s.cwd)
	s.Require().Len(first.Skills, 1)
	s.Equal("alpha", first.Skills[0].Name)

	// Simulate a burst of writes to the same file (e.g. editor save + git checkout).
	for i := range 10 {
		writeTestFile(s.T(), s.skillPath, skillMarkdown("burst-"+strconv.Itoa(i)))
	}

	s.Require().Eventually(func() bool {
		refreshed := s.cache.Get(s.cwd)

		return len(refreshed.Skills) == 1 && refreshed.Skills[0].Name == "burst-9"
	}, 3*time.Second, 50*time.Millisecond)
}
