// Package outputschema implements librecode's bounded structured-output contract.
package outputschema

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Structured-output resource and size limits.
const (
	MaxSchemaBytes     = 64 << 10
	MaxDocumentDepth   = 32
	MaxDocumentValues  = 8192
	MaxSchemaNodes     = 1024
	MaxSchemaMembers   = 4096
	MaxBranches        = 128
	MaxRefs            = 128
	MaxArrayEntries    = 256
	MaxStringBytes     = 16 << 10
	MaxPatternBytes    = 1 << 10
	MaxCandidateBytes  = 256 << 10
	MaxCandidateDepth  = 64
	MaxCandidateValues = 16384
	MaxValueBytes      = 64 << 10
	MaxOutcomeBytes    = 256 << 10
	MaxDiagnosticBytes = 4096
	ResourceURL        = "librecode://output-schema/schema.json"
)

const (
	requiredKeyword    = "required"
	prefixItemsKeyword = "prefixItems"
)

// PersistedPolicy identifies the admission semantics used to restore a durable schema snapshot.
type PersistedPolicy string

const (
	// PersistedPolicyV1 freezes the original structured-output subset and resource limits.
	// New admission behavior must use a new policy; V1 semantics must not be tightened.
	PersistedPolicyV1 PersistedPolicy = "output-schema/v1"
)

type admissionPolicy struct {
	preflight func(map[string]any) error
}

var jsonNumberPattern = regexp.MustCompile(`^-?(0|[1-9]\d*)(\.\d+)?([eE][+-]?\d+)?$`)

// Contract is an admitted, canonical schema and its compiled validator.
type Contract struct {
	schema *jsonschema.Schema
	Digest string

	Canonical []byte
}

// Error is a bounded typed admission or validation failure.
type Error struct{ Code, Reason string }

func (e *Error) Error() string { return e.Code + ": " + e.Reason }

func failure(code, format string, args ...any) error {
	reason := fmt.Sprintf(format, args...)
	if len(reason) > MaxDiagnosticBytes {
		end := MaxDiagnosticBytes
		for !utf8.RuneStart(reason[end]) {
			end--
		}

		reason = reason[:end]
	}

	return &Error{Code: code, Reason: reason}
}

// Admit parses, bounds, preflights, canonicalizes, digests, and compiles a schema.
func Admit(text string) (*Contract, error) {
	return admitWithPolicy(text, admissionPolicy{preflight: preflightV1})
}

func admitWithPolicy(text string, policy admissionPolicy) (*Contract, error) {
	if len(text) > MaxSchemaBytes {
		return nil, failure("invalid_output_schema", "submitted schema bytes exceed limit %d", MaxSchemaBytes)
	}

	if !utf8.ValidString(text) {
		return nil, failure("invalid_output_schema", "schema is not valid UTF-8")
	}

	value, parseErr := parse([]byte(text), MaxDocumentDepth, MaxDocumentValues)
	if parseErr != nil {
		return nil, failure("invalid_output_schema", "%v", parseErr)
	}

	root, ok := value.(map[string]any)
	if !ok {
		return nil, failure("invalid_output_schema", "schema root must be an object")
	}

	if preflightErr := policy.preflight(root); preflightErr != nil {
		return nil, failure("invalid_output_schema", "%v", preflightErr)
	}

	canonical, canonicalErr := canonicalJSON(value)
	if canonicalErr != nil {
		return nil, failure("invalid_output_schema", "%v", canonicalErr)
	}

	if len(canonical) > MaxSchemaBytes {
		return nil, failure("invalid_output_schema", "canonical schema bytes exceed limit %d", MaxSchemaBytes)
	}

	digestBytes := sha256.Sum256(canonical)
	digest := "sha256:" + hex.EncodeToString(digestBytes[:])

	compiled, compileErr := compile(canonical)
	if compileErr != nil {
		return nil, failure("invalid_output_schema", "schema compilation failed: %v", compileErr)
	}

	return &Contract{schema: compiled, Canonical: canonical, Digest: digest}, nil
}

// Restore verifies and compiles a persisted V1 schema.
//
// Deprecated: durable callers should use RestoreWithPolicy and persist the selected policy.
func Restore(canonical []byte, digest string) (*Contract, error) {
	return RestoreWithPolicy(canonical, digest, PersistedPolicyV1)
}

// RestoreWithPolicy restores a durable schema under its frozen admission policy. Compatibility
// handling never rewrites canonical bytes, so digest and canonical identity remain authoritative.
func RestoreWithPolicy(canonical []byte, digest string, persistedPolicy PersistedPolicy) (*Contract, error) {
	if persistedPolicy != PersistedPolicyV1 {
		return nil, failure("invalid_output_schema", "unsupported persisted schema policy %q", persistedPolicy)
	}

	policy := admissionPolicy{preflight: preflightV1}

	contract, err := admitWithPolicy(string(canonical), policy)
	if err != nil {
		return nil, err
	}

	if contract.Digest != digest || !bytes.Equal(contract.Canonical, canonical) {
		return nil, failure("invalid_output_schema", "persisted schema identity mismatch")
	}

	return contract, nil
}

// Validate decodes exactly one bounded JSON value, validates it, and returns canonical bytes.
func (c *Contract) Validate(candidate string) ([]byte, error) {
	if c == nil || c.schema == nil {
		return nil, failure("output_schema_validation_failed", "validator is unavailable")
	}

	if len(candidate) > MaxCandidateBytes {
		return nil, failure(
			"output_schema_validation_failed", "candidate output bytes exceed limit %d", MaxCandidateBytes,
		)
	}

	if !utf8.ValidString(candidate) {
		return nil, failure("output_schema_validation_failed", "candidate is not valid UTF-8")
	}

	value, parseErr := parse([]byte(candidate), MaxCandidateDepth, MaxCandidateValues)
	if parseErr != nil {
		return nil, failure("output_schema_validation_failed", "%v", parseErr)
	}

	if validationErr := c.schema.Validate(value); validationErr != nil {
		return nil, failure("output_schema_validation_failed", "schema assertion failed: %v", validationErr)
	}

	canonical, canonicalErr := canonicalJSON(value)
	if canonicalErr != nil {
		return nil, failure("output_schema_validation_failed", "%v", canonicalErr)
	}

	if len(canonical) > MaxValueBytes {
		return nil, failure(
			"output_schema_validation_failed", "canonical domain-value bytes exceed limit %d", MaxValueBytes,
		)
	}

	return canonical, nil
}

type denyURLLoader struct{}

func (denyURLLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema resource denied: %s", url)
}

func compile(canonical []byte) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(canonical))
	if err != nil {
		return nil, fmt.Errorf("decode canonical schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(denyURLLoader{})

	if err = compiler.AddResource(ResourceURL, doc); err != nil {
		return nil, fmt.Errorf("add schema resource: %w", err)
	}

	compiled, err := compiler.Compile(ResourceURL)
	if err != nil {
		return nil, fmt.Errorf("compile schema resource: %w", err)
	}

	return compiled, nil
}

type boundedParser struct {
	decoder   *json.Decoder
	values    int
	maxDepth  int
	maxValues int
}

func parse(data []byte, maxDepth, maxValues int) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	parser := boundedParser{decoder: decoder, values: 0, maxDepth: maxDepth, maxValues: maxValues}

	value, err := parser.read(1)
	if err != nil {
		return nil, err
	}

	token, err := decoder.Token()
	if err == nil {
		return nil, fmt.Errorf("trailing JSON value %v", token)
	}

	if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read trailing JSON token: %w", err)
	}

	return value, nil
}

func (p *boundedParser) read(depth int) (any, error) {
	if depth > p.maxDepth {
		return nil, fmt.Errorf("JSON depth exceeds limit %d", p.maxDepth)
	}

	token, err := p.decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("read JSON token: %w", err)
	}

	p.values++
	if p.values > p.maxValues {
		return nil, fmt.Errorf("JSON values exceed limit %d", p.maxValues)
	}

	delim, isDelim := token.(json.Delim)
	if isDelim {
		switch delim {
		case '{':
			return p.readObject(depth)
		case '[':
			return p.readArray(depth)
		default:
			return nil, fmt.Errorf("unexpected JSON delimiter %q", delim)
		}
	}

	if text, isString := token.(string); isString && len(text) > MaxStringBytes {
		return nil, fmt.Errorf("JSON string bytes exceed limit %d", MaxStringBytes)
	}

	return token, nil
}

func (p *boundedParser) readObject(depth int) (map[string]any, error) {
	object := make(map[string]any)

	for p.decoder.More() {
		keyToken, err := p.decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("read object key: %w", err)
		}

		key, ok := keyToken.(string)
		if !ok {
			return nil, errors.New("object key is not a string")
		}

		if len(key) > MaxStringBytes {
			return nil, fmt.Errorf("JSON string bytes exceed limit %d", MaxStringBytes)
		}

		if _, exists := object[key]; exists {
			return nil, fmt.Errorf("duplicate object member %q", key)
		}

		item, err := p.read(depth + 1)
		if err != nil {
			return nil, err
		}

		object[key] = item
	}

	if _, err := p.decoder.Token(); err != nil {
		return nil, fmt.Errorf("close JSON object: %w", err)
	}

	return object, nil
}

func (p *boundedParser) readArray(depth int) ([]any, error) {
	array := make([]any, 0)

	for p.decoder.More() {
		item, err := p.read(depth + 1)
		if err != nil {
			return nil, err
		}

		array = append(array, item)
	}

	if _, err := p.decoder.Token(); err != nil {
		return nil, fmt.Errorf("close JSON array: %w", err)
	}

	return array, nil
}

type canonicalEncoder struct{ out bytes.Buffer }

func canonicalJSON(value any) ([]byte, error) {
	encoder := canonicalEncoder{out: bytes.Buffer{}}
	if err := encoder.write(value); err != nil {
		return nil, err
	}

	return encoder.out.Bytes(), nil
}

func (e *canonicalEncoder) write(value any) error {
	switch typed := value.(type) {
	case nil:
		e.out.WriteString("null")
	case bool:
		e.writeBool(typed)
	case string:
		return e.writeString(typed)
	case json.Number:
		return e.writeNumber(typed)
	case []any:
		return e.writeArray(typed)
	case map[string]any:
		return e.writeObject(typed)
	default:
		return fmt.Errorf("unsupported JSON value %T", value)
	}

	return nil
}

func (e *canonicalEncoder) writeBool(value bool) {
	if value {
		e.out.WriteString("true")
	} else {
		e.out.WriteString("false")
	}
}

func (e *canonicalEncoder) writeString(value string) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode JSON string: %w", err)
	}

	e.out.Write(encoded)

	return nil
}

func (e *canonicalEncoder) writeNumber(value json.Number) error {
	if !validJSONNumber(value.String()) {
		return fmt.Errorf("invalid JSON number %q", value)
	}

	e.out.WriteString(value.String())

	return nil
}

func (e *canonicalEncoder) writeArray(array []any) error {
	e.out.WriteByte('[')

	for index, item := range array {
		if index > 0 {
			e.out.WriteByte(',')
		}

		if err := e.write(item); err != nil {
			return err
		}
	}

	e.out.WriteByte(']')

	return nil
}

func (e *canonicalEncoder) writeObject(object map[string]any) error {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}

	sort.Strings(keys)
	e.out.WriteByte('{')

	for index, key := range keys {
		if index > 0 {
			e.out.WriteByte(',')
		}

		if err := e.writeString(key); err != nil {
			return err
		}

		e.out.WriteByte(':')

		if err := e.write(object[key]); err != nil {
			return err
		}
	}

	e.out.WriteByte('}')

	return nil
}

func validJSONNumber(value string) bool {
	return jsonNumberPattern.MatchString(value)
}

// allowedKeywordV1 is part of PersistedPolicyV1. Expand current admission through a new
// policy rather than changing the semantics used to recover existing durable tasks.
func allowedKeywordV1(keyword string) bool {
	switch keyword {
	case "$schema", "$defs", "$ref", "title", "description", "default", "examples", "deprecated",
		"readOnly", "writeOnly", "type", "enum", "const", "allOf", "anyOf", "oneOf", "not", "properties",
		requiredKeyword, "additionalProperties", "minProperties", "maxProperties", "items", prefixItemsKeyword,
		"minItems", "maxItems", "uniqueItems", "minLength", "maxLength", "pattern", "minimum", "maximum",
		"exclusiveMinimum", "exclusiveMaximum", "multipleOf":
		return true
	default:
		return false
	}
}

type preflightState struct {
	defs map[string]any
	used map[string]bool

	nodes    int
	members  int
	branches int
	refs     int
}

func preflightV1(root map[string]any) error {
	state := &preflightState{
		defs: map[string]any{}, used: map[string]bool{}, nodes: 0, members: 0, branches: 0, refs: 0,
	}

	if raw, exists := root["$schema"]; exists && raw != "https://json-schema.org/draft/2020-12/schema" {
		return errors.New("$schema must select draft 2020-12")
	}

	if raw, exists := root["$defs"]; exists {
		defs, ok := raw.(map[string]any)
		if !ok {
			return errors.New("$defs must be an object")
		}

		state.defs = defs
	}

	return walkSchema(root, state, false)
}

func walkSchema(schema any, state *preflightState, insideRefTarget bool) error {
	if err := state.countNode(); err != nil {
		return err
	}

	if _, ok := schema.(bool); ok {
		return nil
	}

	object, ok := schema.(map[string]any)
	if !ok {
		return errors.New("schema position must be an object or boolean")
	}

	if err := state.checkObject(object); err != nil {
		return err
	}

	if err := checkPattern(object); err != nil {
		return err
	}

	if err := state.walkReference(object, insideRefTarget); err != nil {
		return err
	}

	if err := state.walkSchemaArrays(object, insideRefTarget); err != nil {
		return err
	}

	if err := checkValueArrays(object); err != nil {
		return err
	}

	if err := state.walkSchemaMembers(object, insideRefTarget); err != nil {
		return err
	}

	return state.walkDefinitions(object)
}

func (s *preflightState) countNode() error {
	s.nodes++
	if s.nodes > MaxSchemaNodes {
		return fmt.Errorf("schema nodes exceed limit %d", MaxSchemaNodes)
	}

	return nil
}

func (s *preflightState) checkObject(object map[string]any) error {
	s.members += len(object)
	if s.members > MaxSchemaMembers {
		return fmt.Errorf("schema object members exceed limit %d", MaxSchemaMembers)
	}

	for keyword := range object {
		if !allowedKeywordV1(keyword) {
			return fmt.Errorf("unsupported keyword %q", keyword)
		}
	}

	return nil
}

func checkPattern(object map[string]any) error {
	pattern, exists := object["pattern"]
	if !exists {
		return nil
	}

	text, ok := pattern.(string)
	if !ok {
		return errors.New("pattern must be a string")
	}

	if len(text) > MaxPatternBytes {
		return fmt.Errorf("pattern bytes exceed limit %d", MaxPatternBytes)
	}

	if _, err := regexp.Compile(text); err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}

	return nil
}

func (s *preflightState) walkReference(object map[string]any, insideRefTarget bool) error {
	ref, exists := object["$ref"]
	if !exists {
		return nil
	}

	if insideRefTarget {
		return errors.New("referenced $defs target must not contain $ref")
	}

	text, ok := ref.(string)
	if !ok {
		return errors.New("$ref must be a string")
	}

	name, err := refName(text)
	if err != nil {
		return err
	}

	target, exists := s.defs[name]
	if !exists {
		return fmt.Errorf("unresolved $ref %q", text)
	}

	if s.used[name] {
		return fmt.Errorf("$defs target %q is referenced more than once", name)
	}

	s.used[name] = true
	s.refs++

	if s.refs > MaxRefs {
		return fmt.Errorf("$ref occurrences exceed limit %d", MaxRefs)
	}

	return walkSchema(target, s, true)
}

func (s *preflightState) walkSchemaArrays(object map[string]any, insideRefTarget bool) error {
	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		array, err := schemaArray(object, keyword)
		if err != nil {
			return err
		}

		s.branches += len(array)
		if s.branches > MaxBranches {
			return fmt.Errorf("composition branches exceed limit %d", MaxBranches)
		}

		if err := s.walkArray(array, insideRefTarget); err != nil {
			return err
		}
	}

	prefixItems, err := schemaArray(object, prefixItemsKeyword)
	if err != nil {
		return err
	}

	if len(prefixItems) > MaxArrayEntries {
		return fmt.Errorf("%s entries exceed limit %d", prefixItemsKeyword, MaxArrayEntries)
	}

	return s.walkArray(prefixItems, insideRefTarget)
}

func schemaArray(object map[string]any, keyword string) ([]any, error) {
	raw, exists := object[keyword]
	if !exists {
		return nil, nil
	}

	array, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an array", keyword)
	}

	return array, nil
}

func (s *preflightState) walkArray(array []any, insideRefTarget bool) error {
	for _, child := range array {
		if err := walkSchema(child, s, insideRefTarget); err != nil {
			return err
		}
	}

	return nil
}

func checkValueArrays(object map[string]any) error {
	for _, keyword := range []string{"enum", requiredKeyword} {
		raw, exists := object[keyword]
		if !exists {
			continue
		}

		array, ok := raw.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", keyword)
		}

		if len(array) > MaxArrayEntries {
			return fmt.Errorf("%s entries exceed limit %d", keyword, MaxArrayEntries)
		}

		if keyword == requiredKeyword {
			if err := uniqueStrings(array, keyword); err != nil {
				return err
			}
		}
	}

	rawType, exists := object["type"]
	if !exists {
		return nil
	}

	types, isArray := rawType.([]any)
	if !isArray {
		return nil
	}

	if len(types) == 0 {
		return errors.New("type array must not be empty")
	}

	return uniqueStrings(types, "type")
}

func (s *preflightState) walkSchemaMembers(object map[string]any, insideRefTarget bool) error {
	for _, keyword := range []string{"not", "items", "additionalProperties"} {
		child, exists := object[keyword]
		if !exists {
			continue
		}

		if err := walkSchema(child, s, insideRefTarget); err != nil {
			return fmt.Errorf("%s: %w", keyword, err)
		}
	}

	raw, exists := object["properties"]
	if !exists {
		return nil
	}

	properties, ok := raw.(map[string]any)
	if !ok {
		return errors.New("properties must be an object")
	}

	for _, child := range properties {
		if err := walkSchema(child, s, insideRefTarget); err != nil {
			return err
		}
	}

	return nil
}

func (s *preflightState) walkDefinitions(object map[string]any) error {
	raw, exists := object["$defs"]
	if !exists {
		return nil
	}

	defs, ok := raw.(map[string]any)
	if !ok {
		return errors.New("$defs must be an object")
	}

	for name, child := range defs {
		if s.used[name] {
			continue
		}

		if err := walkSchema(child, s, false); err != nil {
			return err
		}
	}

	return nil
}

func uniqueStrings(array []any, keyword string) error {
	seen := make(map[string]bool, len(array))

	for _, item := range array {
		text, ok := item.(string)
		if !ok {
			return fmt.Errorf("%s entries must be strings", keyword)
		}

		if seen[text] {
			return fmt.Errorf("%s contains duplicate %q", keyword, text)
		}

		seen[text] = true
	}

	return nil
}

func refName(ref string) (string, error) {
	const prefix = "#/$defs/"

	if !strings.HasPrefix(ref, prefix) {
		return "", fmt.Errorf("unsupported $ref %q", ref)
	}

	token := strings.TrimPrefix(ref, prefix)
	if token == "" || strings.Contains(token, "/") || strings.ContainsAny(token, "%?#") {
		return "", fmt.Errorf("unsupported $ref %q", ref)
	}

	name, err := decodePointerToken(token)
	if err != nil {
		return "", fmt.Errorf("invalid $ref escape in %q", ref)
	}

	return name, nil
}

func decodePointerToken(token string) (string, error) {
	var out strings.Builder

	for index := 0; index < len(token); index++ {
		if token[index] != '~' {
			out.WriteByte(token[index])

			continue
		}

		if index+1 >= len(token) || (token[index+1] != '0' && token[index+1] != '1') {
			return "", errors.New("invalid JSON pointer escape")
		}

		index++
		if token[index] == '0' {
			out.WriteByte('~')
		} else {
			out.WriteByte('/')
		}
	}

	return out.String(), nil
}
