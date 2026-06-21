package tool

import "reflect"

const (
	schemaPathField                = "Path"
	schemaLimitField               = "Limit"
	schemaAllowIgnoredField        = "AllowIgnored"
	schemaRelativePathSuffix       = "relative to the current workspace or absolute."
	schemaSourcePathDescription    = "Path to the source file to inspect, " + schemaRelativePathSuffix
	schemaWorkspacePathDescription = "Path to the file to read, " + schemaRelativePathSuffix
)

// inputSchemaForName returns the generated JSON Schema for a built-in tool input.
func inputSchemaForName(name Name) Schema {
	inputType, ok := inputTypesByName()[name]
	if !ok {
		return EmptySchema()
	}

	schema, err := schemaFromType(inputType, lookupSchemaFieldComment)
	if err != nil {
		panic(err)
	}

	return schema
}

func inputTypesByName() map[Name]reflect.Type {
	return map[Name]reflect.Type{
		NameRead:  reflect.TypeFor[ReadInput](),
		NameBash:  reflect.TypeFor[BashInput](),
		NameEdit:  reflect.TypeFor[EditInput](),
		NameWrite: reflect.TypeFor[WriteInput](),
		NameGrep:  reflect.TypeFor[GrepInput](),
		NameFind:  reflect.TypeFor[FindInput](),
		NameLS:    reflect.TypeFor[LSInput](),
		NameAST:   reflect.TypeFor[ASTInput](),
	}
}

func lookupSchemaFieldComment(inputType reflect.Type, fieldName string) string {
	if fieldName == "" {
		return ""
	}

	comments, ok := schemaFieldComments()[inputType]
	if !ok {
		return ""
	}

	return comments[fieldName]
}

func schemaFieldComments() map[reflect.Type]map[string]string {
	return map[reflect.Type]map[string]string{
		reflect.TypeFor[ASTInput](): {
			"Line":                  "One-based line number for mode=node or mode=tree.",
			"Depth":                 "Optional recursion depth for mode=symbols.",
			schemaPathField:         schemaSourcePathDescription,
			"Mode":                  "Inspection mode: 'outline' (default), 'symbols', 'query', 'node', or 'tree'.",
			"Query":                 "Tree-sitter S-expression query for mode=query.",
			schemaAllowIgnoredField: allowIgnoredSchemaDescription(),
		},
		reflect.TypeFor[BashInput](): {
			"Timeout": "Optional timeout in seconds.",
			"Command": "Bash command to execute in the current workspace.",
		},
		reflect.TypeFor[EditInput](): {
			schemaPathField: "Path to the file to edit, relative to the current workspace or absolute.",
		},
		reflect.TypeFor[FindInput](): {
			schemaLimitField: "Optional maximum number of paths.",
			"Pattern":        "Glob pattern for file paths, such as **/*.go.",
			schemaPathField:  "Optional directory to search under.",
		},
		reflect.TypeFor[GrepInput](): {
			"Context":        "Optional number of context lines around each match.",
			schemaLimitField: "Optional maximum number of matches.",
			"Pattern":        "Regular expression or literal string to search for.",
			schemaPathField:  "Optional file or directory to search under.",
			"Glob":           "Optional glob filter such as **/*.go.",
			"IgnoreCase":     "Whether to match case-insensitively.",
			"Literal":        "Whether pattern should be treated as literal text.",
		},
		reflect.TypeFor[LSInput](): {
			schemaLimitField: "Optional maximum number of entries.",
			schemaPathField:  "Optional directory to list.",
		},
		reflect.TypeFor[ReadInput](): {
			"Offset":                "Optional 1-indexed line number to start reading from.",
			schemaLimitField:        "Optional maximum number of lines to return.",
			schemaPathField:         schemaWorkspacePathDescription,
			schemaAllowIgnoredField: allowIgnoredSchemaDescription(),
		},
		reflect.TypeFor[Replacement](): {
			"OldText": "Exact text to replace. Must match a unique, non-overlapping region.",
			"NewText": "Replacement text.",
		},
		reflect.TypeFor[WriteInput](): {
			"Content":       "Complete file content to write.",
			schemaPathField: "Path to create or overwrite, relative to the current workspace or absolute.",
		},
	}
}

func allowIgnoredSchemaDescription() string {
	return "Set true only when an ignored file is explicitly needed despite .gitignore/default ignores."
}
