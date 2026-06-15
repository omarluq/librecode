package terminal

import "time"

const (
	minimumAuthCommandFields    = 2
	maxAutocompleteMatches      = 6
	slashAutocompleteLabelWidth = 16
	compactStageNear            = 2
	compactStageFar             = 3
	clipboardTimeout            = 2 * time.Second
	minimumRuleWidth            = 3
	maxMarkdownHeadingLevel     = 6
	tableCellHorizontalPadding  = 2
	defaultTerminalWidth        = 80
	defaultTerminalHeight       = 24
	terminalHalf                = 2
	messageHorizontalPadding    = 2
	messageBoxHorizontalPadding = messageHorizontalPadding * 2
	messageVerticalPadding      = 2
	messageOuterRows            = 2
	messageMetadataRows         = 3
	assistantMessageExtraRows   = 5
	defaultMessageExtraRows     = 4
	sessionTreePreviewWidth     = 80
	minimumComposerHeight       = 3
	composerBorderRows          = 2
	runtimeBorderWidth          = 2
	terminalMarkerMargin        = 2
	initialToolBlockLines       = 10
	toolBlockBorderWidth        = 2
	toolHeaderLines             = 2
	welcomeDoublePadding        = 2
	welcomeArtExtraLines        = 8
)
