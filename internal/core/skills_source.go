package core

import "path/filepath"

func sourceInfoForSkill(filePath, cwd string) SourceInfo {
	scope := SourceScopeTemporary
	baseDir := filepath.Dir(filePath)
	for _, projectSkillsDir := range projectSkillPaths(cwd) {
		if isUnderPath(filePath, projectSkillsDir) {
			scope = SourceScopeProject
			baseDir = projectSkillsDir
			break
		}
	}
	if scope == SourceScopeTemporary {
		for _, userSkillsDir := range userSkillPaths() {
			if isUnderPath(filePath, userSkillsDir) {
				scope = SourceScopeUser
				baseDir = userSkillsDir
				break
			}
		}
	}

	return NewSourceInfo(filePath, SourceInfoOptions{
		Scope:   scope,
		Origin:  SourceOriginTopLevel,
		BaseDir: baseDir,
		Source:  resourceSourceLocal,
	})
}
