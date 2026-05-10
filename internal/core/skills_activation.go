package core

import (
	"sort"
	"strings"
	"unicode"

	"github.com/samber/lo"
)

// SkillActivationDiagnostic explains why a skill was automatically activated.
type SkillActivationDiagnostic struct {
	Reason string `json:"reason"`
	Skill  Skill  `json:"skill"`
	Score  int    `json:"score"`
}

// SkillActivationResult returns activated skill content plus activation diagnostics.
type SkillActivationResult struct {
	Activated   []ActivatedSkill            `json:"activated"`
	Diagnostics []ResourceDiagnostic        `json:"diagnostics"`
	Matches     []SkillActivationDiagnostic `json:"matches"`
}

type rankedSkill struct {
	reason string
	skill  Skill
	score  int
	order  int
}

// AutoActivateSkills selects matching skills and reads their SKILL.md content for prompt context.
func AutoActivateSkills(prompt string, skills []Skill) ([]ActivatedSkill, []ResourceDiagnostic) {
	result := AutoActivateSkillsDetailed(prompt, skills)

	return result.Activated, result.Diagnostics
}

// AutoActivateSkillsDetailed selects matching skills and returns activation reasons for diagnostics.
func AutoActivateSkillsDetailed(prompt string, skills []Skill) SkillActivationResult {
	ranked := rankSkillsForPrompt(prompt, skills)
	if len(ranked) == 0 {
		return SkillActivationResult{
			Activated:   []ActivatedSkill{},
			Diagnostics: []ResourceDiagnostic{},
			Matches:     []SkillActivationDiagnostic{},
		}
	}
	if len(ranked) > maxActiveSkills {
		ranked = ranked[:maxActiveSkills]
	}

	activated := make([]ActivatedSkill, 0, len(ranked))
	diagnostics := []ResourceDiagnostic{}
	matches := make([]SkillActivationDiagnostic, 0, len(ranked))
	for index := range ranked {
		match := &ranked[index]
		content, err := readResourceFile(match.skill.FilePath)
		if err != nil {
			diagnostics = append(diagnostics, warningDiagnostic(err.Error(), match.skill.FilePath))
			continue
		}
		content, truncated := truncateSkillContent(content)
		activated = append(activated, ActivatedSkill{Skill: match.skill, Content: content, Truncated: truncated})
		matches = append(matches, SkillActivationDiagnostic{
			Skill:  match.skill,
			Reason: match.reason,
			Score:  match.score,
		})
	}

	return SkillActivationResult{Activated: activated, Diagnostics: diagnostics, Matches: matches}
}

func rankSkillsForPrompt(prompt string, skills []Skill) []rankedSkill {
	ranked := []rankedSkill{}
	promptTokens := normalizedTokenSet(prompt)
	promptLower := strings.ToLower(prompt)
	for index := range skills {
		skill := skills[index]
		if skill.DisableModelInvocation {
			continue
		}
		score, reason := skillActivationScore(promptLower, promptTokens, &skill)
		if score == 0 {
			continue
		}
		ranked = append(ranked, rankedSkill{skill: skill, reason: reason, score: score, order: index})
	}
	sort.SliceStable(ranked, func(leftIndex, rightIndex int) bool {
		left := ranked[leftIndex]
		right := ranked[rightIndex]
		if left.score == right.score {
			return left.order < right.order
		}

		return left.score > right.score
	})

	return ranked
}

func skillActivationScore(promptLower string, promptTokens map[string]bool, skill *Skill) (score int, reason string) {
	nameLower := strings.ToLower(skill.Name)
	if score, reason := skillNameActivationScore(promptLower, promptTokens, nameLower); score > 0 {
		return score, reason
	}

	return skillDescriptionActivationScore(promptTokens, skill.Description)
}

func skillNameActivationScore(
	promptLower string,
	promptTokens map[string]bool,
	nameLower string,
) (score int, reason string) {
	if containsSkillPhrase(promptLower, nameLower) {
		return 100, "exact skill name mention"
	}

	nameTokens := normalizedTokens(nameLower)
	if len(nameTokens) == 0 {
		return 0, ""
	}
	for _, token := range nameTokens {
		if !promptTokens[token] {
			return 0, ""
		}
	}

	return 80, "all skill name tokens matched"
}

func skillDescriptionActivationScore(promptTokens map[string]bool, description string) (score int, reason string) {
	bestScore := 0
	bestReason := ""
	for _, phrase := range skillActivationPhrases(description) {
		phraseTokens := lo.Filter(normalizedTokens(phrase), func(token string, _ int) bool {
			return !isSkillStopWord(token)
		})
		phraseTokens = lo.Uniq(phraseTokens)
		if len(phraseTokens) == 0 {
			continue
		}

		matches := 0
		matchedTokens := []string{}
		for _, token := range phraseTokens {
			if promptTokens[token] {
				matches++
				matchedTokens = append(matchedTokens, token)
			}
		}
		if matches < 2 {
			continue
		}

		score := 40 + matches
		if score > bestScore {
			bestScore = score
			bestReason = "activation phrase matched tokens: " + strings.Join(matchedTokens, ", ")
		}
	}

	return bestScore, bestReason
}

func containsSkillPhrase(inputLower, phraseLower string) bool {
	phraseTokens := normalizedTokens(phraseLower)
	if len(phraseTokens) == 0 {
		return false
	}

	inputTokens := normalizedTokens(inputLower)
	for index := 0; index+len(phraseTokens) <= len(inputTokens); index++ {
		matched := true
		for offset, phraseToken := range phraseTokens {
			if inputTokens[index+offset] != phraseToken {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}

	return false
}

func skillActivationPhrases(description string) []string {
	phrases := []string{}
	for _, sentence := range skillActivationSentencePattern.Split(description, -1) {
		for clause := range strings.SplitSeq(sentence, ",") {
			trimmed := strings.TrimSpace(clause)
			if trimmed == "" {
				continue
			}
			if isActivationPhrase(trimmed) {
				phrases = append(phrases, trimmed)
			}
		}
	}

	return phrases
}

func isActivationPhrase(phrase string) bool {
	phraseLower := strings.ToLower(phrase)
	markers := []string{
		"apply when", "also trigger", "also triggers", "trigger on", "triggers on",
		"use for", "use this", "use when", "whenever", "when ",
	}
	for _, marker := range markers {
		if strings.Contains(phraseLower, marker) {
			return true
		}
	}

	return false
}

func normalizedTokenSet(input string) map[string]bool {
	tokens := map[string]bool{}
	for _, token := range normalizedTokens(input) {
		if token != "" {
			tokens[token] = true
		}
	}

	return tokens
}

func normalizedTokens(input string) []string {
	fields := strings.FieldsFunc(strings.ToLower(input), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		if field == "" {
			continue
		}
		tokens = append(tokens, normalizeSkillToken(field))
	}

	return tokens
}

func normalizeSkillToken(token string) string {
	switch token {
	case "golang":
		return "go"
	case "writing":
		return "write"
	case "uses", "used", "using":
		return "use"
	}

	for _, suffix := range []string{"ing", "ed", "es", "s"} {
		if len(token) > len(suffix)+2 && strings.HasSuffix(token, suffix) {
			return strings.TrimSuffix(token, suffix)
		}
	}

	return token
}

func isSkillStopWord(token string) bool {
	stopWords := map[string]bool{
		"about": true, "after": true, "agent": true, "also": true, "and": true,
		"any": true, "apply": true, "are": true, "build": true, "can": true,
		"code": true, "coding": true, "cover": true, "covers": true, "debug": true,
		"designed": true, "especially": true, "for": true, "from": true, "guide": true,
		"helps": true, "implement": true, "into": true, "not": true, "only": true,
		"project": true, "provides": true, "review": true, "similar": true, "skill": true,
		"task": true, "tasks": true, "that": true, "the": true, "their": true, "these": true,
		"this": true, "tool": true, "tools": true, "trigger": true, "use": true,
		"when": true, "whenever": true, "with": true, "work": true, "working": true,
		"write": true, "you": true,
	}

	return stopWords[token]
}

func truncateSkillContent(content string) (string, bool) {
	runes := []rune(content)
	if len(runes) <= maxActiveSkillContent {
		return content, false
	}

	return string(runes[:maxActiveSkillContent]) + "\n\n[skill content truncated]", true
}
