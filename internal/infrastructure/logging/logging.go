// Package logging provides structured logs with fail-closed sensitive attributes.
package logging

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// Redacted replaces every sensitive log value.
const Redacted = "[REDACTED]"

var sensitiveNames = map[string]struct{}{
	"accesstoken":        {},
	"apikey":             {},
	"authorization":      {},
	"bearer":             {},
	"clientsecret":       {},
	"cookie":             {},
	"idtoken":            {},
	"proxyauthorization": {},
	"refreshtoken":       {},
	"secret":             {},
	"setcookie":          {},
	"token":              {},
	"xapikey":            {},
}

var timeType = reflect.TypeOf(time.Time{})

var (
	headerPattern       = regexp.MustCompile(`(?im)\b(?:Proxy-Authorization|Authorization|Set-Cookie|Cookie)[\t ]*:[^\r\n]*`)
	knownAPIKeyPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`),
		regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{20,}\b`),
		regexp.MustCompile(`\bgh[pousr]_[0-9A-Za-z]{20,}\b`),
		regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	}
	queryParameterPattern = regexp.MustCompile(`([?&])([^=&\s#]+)=([^&#\s]*)`)
)

// NewRedactingJSONHandler returns a JSON handler that redacts sensitive values recursively.
func NewRedactingJSONHandler(writer io.Writer, options *slog.HandlerOptions) slog.Handler {
	return &redactingHandler{next: slog.NewJSONHandler(writer, options)}
}

// Open creates or appends to the private application JSON log.
func Open(logDirectory string) (*slog.Logger, io.Closer, error) {
	file, err := os.OpenFile(filepath.Join(logDirectory, "app.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, logOpenError(err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, nil, logOpenError(err)
	}
	return slog.New(NewRedactingJSONHandler(file, nil)), file, nil
}

type redactingHandler struct {
	next            slog.Handler
	parentSensitive bool
}

func (h *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	redacted := slog.NewRecord(record.Time, record.Level, sanitizeText(record.Message), record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		redacted.AddAttrs(redactAttr(attr, h.parentSensitive))
		return true
	})
	return h.next.Handle(ctx, redacted)
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		redacted = append(redacted, redactAttr(attr, h.parentSensitive))
	}
	return &redactingHandler{next: h.next.WithAttrs(redacted), parentSensitive: h.parentSensitive}
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{
		next:            h.next.WithGroup(name),
		parentSensitive: h.parentSensitive || isSensitiveName(name),
	}
}

func redactAttr(attr slog.Attr, parentSensitive bool) slog.Attr {
	if parentSensitive || isSensitiveName(attr.Key) {
		return slog.String(attr.Key, Redacted)
	}

	value := attr.Value.Resolve()
	if value.Kind() == slog.KindGroup {
		group := value.Group()
		redacted := make([]slog.Attr, 0, len(group))
		for _, child := range group {
			redacted = append(redacted, redactAttr(child, false))
		}
		return slog.Group(attr.Key, redactedToAny(redacted)...)
	}
	if value.Kind() == slog.KindAny {
		return slog.Any(attr.Key, redactAny(value.Any()))
	}
	if value.Kind() == slog.KindString {
		return slog.String(attr.Key, sanitizeText(value.String()))
	}
	return slog.Attr{Key: attr.Key, Value: value}
}

func redactedToAny(attrs []slog.Attr) []any {
	values := make([]any, len(attrs))
	for index := range attrs {
		values[index] = attrs[index]
	}
	return values
}

func redactAny(value any) any {
	return redactReflect(reflect.ValueOf(value), make(map[visit]bool))
}

type visit struct {
	typeName reflect.Type
	pointer  uintptr
}

// Reflection is deliberately limited to JSON-like maps, sequences, pointers,
// and exported struct fields. It is not a general serializer: errors, byte
// slices, cycles, and opaque aggregates are redacted instead of interpreted.
func redactReflect(value reflect.Value, seen map[visit]bool) any {
	if !value.IsValid() {
		return nil
	}
	if value.CanInterface() {
		if _, ok := value.Interface().(error); ok {
			return Redacted
		}
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		return redactReflect(value.Elem(), seen)
	}

	switch value.Kind() {
	case reflect.Map:
		if value.IsNil() {
			return nil
		}
		if value.Type().Key().Kind() != reflect.String {
			return Redacted
		}
		pointer := value.Pointer()
		key := visit{typeName: value.Type(), pointer: pointer}
		if pointer != 0 && seen[key] {
			return Redacted
		}
		if pointer != 0 {
			seen[key] = true
			defer delete(seen, key)
		}
		result := make(map[string]any, value.Len())
		iterator := value.MapRange()
		for iterator.Next() {
			name := iterator.Key().String()
			if isSensitiveName(name) {
				result[name] = Redacted
				continue
			}
			result[name] = redactReflect(iterator.Value(), seen)
		}
		return result
	case reflect.Slice:
		if value.IsNil() {
			return nil
		}
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return Redacted
		}
		pointer := value.Pointer()
		key := visit{typeName: value.Type(), pointer: pointer}
		if pointer != 0 && seen[key] {
			return Redacted
		}
		if pointer != 0 {
			seen[key] = true
			defer delete(seen, key)
		}
		result := make([]any, value.Len())
		for index := range result {
			result[index] = redactReflect(value.Index(index), seen)
		}
		return result
	case reflect.Array:
		result := make([]any, value.Len())
		for index := range result {
			result[index] = redactReflect(value.Index(index), seen)
		}
		return result
	case reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		pointer := value.Pointer()
		key := visit{typeName: value.Type(), pointer: pointer}
		if pointer != 0 && seen[key] {
			return Redacted
		}
		if pointer != 0 {
			seen[key] = true
			defer delete(seen, key)
		}
		return redactReflect(value.Elem(), seen)
	case reflect.Struct:
		if value.Type() == timeType && value.CanInterface() {
			return value.Interface()
		}
		return redactStruct(value, seen)
	case reflect.String:
		return sanitizeText(value.String())
	default:
		if value.CanInterface() {
			return value.Interface()
		}
		return Redacted
	}
}

func redactStruct(value reflect.Value, seen map[visit]bool) any {
	result := make(map[string]any)
	typeInfo := value.Type()
	for index := 0; index < value.NumField(); index++ {
		fieldInfo := typeInfo.Field(index)
		if fieldInfo.PkgPath != "" {
			continue
		}

		name, omit, omitEmpty := jsonField(fieldInfo)
		if omit {
			continue
		}
		fieldValue := value.Field(index)
		if omitEmpty && fieldValue.IsZero() {
			continue
		}

		redacted := redactReflect(fieldValue, seen)
		if fieldInfo.Anonymous && name == "" {
			if embedded, ok := redacted.(map[string]any); ok {
				for embeddedName, embeddedValue := range embedded {
					result[embeddedName] = embeddedValue
				}
				continue
			}
		}
		if name == "" {
			name = fieldInfo.Name
		}
		if isSensitiveName(name) || isSensitiveName(fieldInfo.Name) {
			result[name] = Redacted
			continue
		}
		result[name] = redacted
	}
	return result
}

func jsonField(field reflect.StructField) (name string, omit bool, omitEmpty bool) {
	tag, exists := field.Tag.Lookup("json")
	if !exists {
		return "", false, false
	}
	parts := strings.Split(tag, ",")
	if parts[0] == "-" {
		return "", true, false
	}
	for _, option := range parts[1:] {
		if option == "omitempty" || option == "omitzero" {
			omitEmpty = true
		}
	}
	return parts[0], false, omitEmpty
}

func isSensitiveName(name string) bool {
	normalized := normalizeSensitiveName(name)
	if _, sensitive := sensitiveNames[normalized]; sensitive {
		return true
	}
	for _, suffix := range []string{"apikey", "authorization", "password", "token", "secret", "cookie", "privatekey", "encryptionkey"} {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

func isSensitiveQueryName(name string) bool {
	normalized := normalizeSensitiveName(name)
	return normalized == "key" || isSensitiveName(name)
}

func normalizeSensitiveName(name string) string {
	return strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(strings.ToLower(name))
}

func sanitizeText(value string) string {
	value = headerPattern.ReplaceAllStringFunc(value, func(header string) string {
		separator := strings.IndexByte(header, ':')
		if separator < 0 {
			return Redacted
		}
		return header[:separator+1] + Redacted
	})
	value = redactBearerValues(value)
	value = queryParameterPattern.ReplaceAllStringFunc(value, func(parameter string) string {
		matches := queryParameterPattern.FindStringSubmatch(parameter)
		if len(matches) != 4 {
			return Redacted
		}
		name, err := url.QueryUnescape(matches[2])
		if err != nil {
			return matches[1] + matches[2] + "=" + Redacted
		}
		if !isSensitiveQueryName(name) {
			return parameter
		}
		return matches[1] + matches[2] + "=" + Redacted
	})
	value = redactAssignments(value)
	for _, pattern := range knownAPIKeyPatterns {
		value = pattern.ReplaceAllString(value, Redacted)
	}
	return value
}

func redactBearerValues(value string) string {
	const label = "Bearer"
	var output strings.Builder
	last := 0
	for index := 0; index+len(label) <= len(value); {
		if !strings.EqualFold(value[index:index+len(label)], label) ||
			(index > 0 && isIdentifierByte(value[index-1])) {
			index++
			continue
		}

		space := index + len(label)
		if space >= len(value) || (value[space] != ' ' && value[space] != '\t') {
			index++
			continue
		}
		credentialStart := skipHorizontalSpace(value, space)
		if credentialStart >= len(value) || isValueBoundary(value[credentialStart]) {
			index = credentialStart
			continue
		}
		credentialEnd := scanCredentialValue(value, credentialStart)
		output.WriteString(value[last:credentialStart])
		output.WriteString(Redacted)
		last = credentialEnd
		index = credentialEnd
	}
	if last == 0 {
		return value
	}
	output.WriteString(value[last:])
	return output.String()
}

func redactAssignments(value string) string {
	// This is intentionally a narrow, single-pass key/separator/value scanner,
	// not a general JSON or configuration parser. Ambiguous values are consumed
	// to a safe structural boundary so credentials fail closed.
	var output strings.Builder
	last := 0
	for index := 0; index < len(value); {
		key, keyEnd, candidate, decoded, keyWasQuoted := scanAssignmentKey(value, index)
		if !candidate {
			index++
			continue
		}
		separator := skipHorizontalSpace(value, keyEnd)
		if separator >= len(value) || (value[separator] != '=' && value[separator] != ':') || (decoded && !isSensitiveName(key)) {
			if keyWasQuoted {
				// Keep raw escapes and the outer quotes. Each contained span excludes
				// its unescaped quote type, so the two quote types bound rescanning
				// to two levels and keep the scan linear without a new parser.
				inner := value[index+1 : keyEnd-1]
				if redacted := redactAssignments(inner); redacted != inner {
					output.WriteString(value[last : index+1])
					output.WriteString(redacted)
					last = keyEnd - 1
				}
			}
			index = keyEnd
			continue
		}

		credentialStart := skipHorizontalSpace(value, separator+1)
		if credentialStart >= len(value) || isEmptyAssignmentValue(value[credentialStart]) {
			index = credentialStart
			continue
		}
		if atomEnd, safe := redactedAssignmentAtomEnd(value, credentialStart, key, keyWasQuoted); safe {
			index = atomEnd
			continue
		}
		credentialEnd := scanAssignmentValue(value, credentialStart, key)
		output.WriteString(value[last:credentialStart])
		output.WriteString(Redacted)
		last = credentialEnd
		index = credentialEnd
	}
	if last == 0 {
		return value
	}
	output.WriteString(value[last:])
	return output.String()
}

func scanAssignmentKey(value string, start int) (key string, end int, candidate bool, decoded bool, quoted bool) {
	if start >= len(value) {
		return "", start, false, false, false
	}
	if quote := value[start]; quote == '\'' || quote == '"' {
		if isEscaped(value, start) || (start > 0 && isIdentifierByte(value[start-1])) {
			return "", start, false, false, false
		}
		end, closed := scanQuotedEnd(value, start)
		if !closed {
			return "", start, false, false, false
		}
		key, decoded := decodeQuotedKey(value[start:end])
		return key, end, true, decoded, true
	}
	if !isIdentifierByte(value[start]) || (start > 0 && isIdentifierByte(value[start-1])) {
		return "", start, false, false, false
	}
	end = start
	for end < len(value) && isIdentifierByte(value[end]) {
		end++
	}
	return value[start:end], end, true, true, false
}

func decodeQuotedKey(quoted string) (string, bool) {
	if len(quoted) < 2 {
		return "", false
	}
	if quoted[0] == '"' {
		decoded, err := strconv.Unquote(quoted)
		return decoded, err == nil
	}

	var decoded strings.Builder
	remaining := quoted[1 : len(quoted)-1]
	for remaining != "" {
		character, _, tail, err := strconv.UnquoteChar(remaining, '\'')
		if err != nil {
			return "", false
		}
		decoded.WriteRune(character)
		remaining = tail
	}
	return decoded.String(), true
}

func redactedAssignmentAtomEnd(value string, start int, key string, keyWasQuoted bool) (int, bool) {
	end := start + len(Redacted)
	if end > len(value) || value[start:end] != Redacted {
		return start, false
	}
	if end == len(value) {
		return end, true
	}
	if isHeaderLikeName(key) {
		if keyWasQuoted && value[end] == ',' {
			return end, true
		}
		return end, isHeaderLikeBoundary(value[end])
	}
	return end, isAssignmentBoundary(value[end])
}

func scanAssignmentValue(value string, start int, key string) int {
	if value[start] == '\'' || value[start] == '"' {
		end, _ := scanQuotedEnd(value, start)
		return end
	}
	scanStart := start
	if strings.HasPrefix(value[start:], Redacted) {
		scanStart += len(Redacted)
	}
	if isHeaderLikeName(key) {
		return scanUntil(value, scanStart, isHeaderLikeBoundary)
	}
	return scanUntil(value, scanStart, isAssignmentBoundary)
}

func scanQuotedEnd(value string, start int) (int, bool) {
	quote := value[start]
	for index := start + 1; index < len(value); index++ {
		if value[index] == '\r' || value[index] == '\n' {
			return index, false
		}
		if value[index] == quote && !isEscaped(value, index) {
			return index + 1, true
		}
	}
	return len(value), false
}

func scanUntil(value string, start int, boundary func(byte) bool) int {
	index := start
	for index < len(value) && !boundary(value[index]) {
		index++
	}
	return index
}

func isHeaderLikeName(name string) bool {
	normalized := normalizeSensitiveName(name)
	for _, suffix := range []string{"authorization", "proxyauthorization", "cookie", "setcookie"} {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
}

func isHeaderLikeBoundary(value byte) bool {
	switch value {
	case '\r', '\n', '}', ']':
		return true
	default:
		return false
	}
}

func isAssignmentBoundary(value byte) bool {
	switch value {
	case '\r', '\n', ',', ';', '&', '#', '}', ']', ')':
		return true
	default:
		return false
	}
}

func isEmptyAssignmentValue(value byte) bool {
	switch value {
	case '\r', '\n', ',', ';', '&', '#', '}', ']':
		return true
	default:
		return false
	}
}

func scanCredentialValue(value string, start int) int {
	if start >= len(value) {
		return start
	}
	if quote := value[start]; quote == '\'' || quote == '"' {
		end, _ := scanQuotedEnd(value, start)
		return end
	}
	index := start
	for index < len(value) && !isValueBoundary(value[index]) {
		index++
	}
	return index
}

func isEscaped(value string, index int) bool {
	backslashes := 0
	for index--; index >= 0 && value[index] == '\\'; index-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func skipHorizontalSpace(value string, index int) int {
	for index < len(value) && (value[index] == ' ' || value[index] == '\t') {
		index++
	}
	return index
}

func isIdentifierByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_' || value == '-' || value == '.'
}

func isValueBoundary(value byte) bool {
	switch value {
	case ' ', '\t', '\r', '\n', ',', ';', '&', '#':
		return true
	default:
		return false
	}
}

func logOpenError(cause error) error {
	return apperror.New(
		"LOG_OPEN_FAILED",
		"storage",
		false,
		"Application logging could not be started.",
		cause,
	)
}
