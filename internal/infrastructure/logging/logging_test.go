package logging

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type secretValue struct{}

func (secretValue) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("ToKeN", "logvaluer-token-value"),
		slog.String("ordinary", "logvaluer-public-value"),
	)
}

type structuredMetadata struct {
	APIKey     string `json:"api_key"`
	Visible    string `json:"visible"`
	Ignored    string `json:"-"`
	unexported string
}

type cyclicMetadata struct {
	Token string          `json:"token"`
	Next  *cyclicMetadata `json:"next"`
	At    time.Time       `json:"at"`
	Err   error           `json:"error"`
}

type extendedSecrets struct {
	OpenAIAPIKey string         `json:"openai_api_key"`
	Nested       map[string]any `json:"nested"`
}

func TestRedactingHandlerRemovesSensitiveValuesRecursively(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewRedactingJSONHandler(&output, nil))
	logger = logger.WithGroup("request").With(slog.String("Authorization", "with-attrs-auth-value"))

	logger.InfoContext(context.Background(), "test",
		slog.String("apiKey", "api-key-value"),
		slog.String("ordinary", "ordinary-value"),
		slog.Group("credentials",
			slog.String("SECRET", "nested-secret-value"),
			slog.String("visible", "nested-public-value"),
		),
		slog.Group("Cookie",
			slog.String("session", "sensitive-group-value"),
		),
		slog.Any("resolved", secretValue{}),
		slog.Any("metadata", map[string]any{
			"refresh_token": "map-token-value",
			"visible":       "map-public-value",
		}),
	)

	got := output.String()
	for _, secret := range []string{
		"with-attrs-auth-value",
		"api-key-value",
		"nested-secret-value",
		"sensitive-group-value",
		"logvaluer-token-value",
		"map-token-value",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("log output exposed %q: %s", secret, got)
		}
	}
	if count := strings.Count(got, Redacted); count < 6 {
		t.Fatalf("redaction count = %d, want at least 6: %s", count, got)
	}
	for _, ordinary := range []string{"ordinary-value", "nested-public-value", "logvaluer-public-value", "map-public-value"} {
		if !strings.Contains(got, ordinary) {
			t.Fatalf("ordinary field %q was lost: %s", ordinary, got)
		}
	}
}

func TestRedactingHandlerRemovesSensitiveStructFieldsRecursively(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewRedactingJSONHandler(&output, nil))
	metadata := structuredMetadata{
		APIKey:     "direct-struct-secret",
		Visible:    "direct-struct-public",
		Ignored:    "ignored-struct-value",
		unexported: "unexported-struct-value",
	}

	logger.Info("structs",
		slog.Any("direct", metadata),
		slog.Any("pointer", &structuredMetadata{APIKey: "pointer-struct-secret", Visible: "pointer-struct-public"}),
		slog.Any("slice", []structuredMetadata{{APIKey: "slice-struct-secret", Visible: "slice-struct-public"}}),
		slog.Any("array", [1]structuredMetadata{{APIKey: "array-struct-secret", Visible: "array-struct-public"}}),
		slog.Any("map", map[string]any{
			"nested": structuredMetadata{APIKey: "map-struct-secret", Visible: "map-struct-public"},
		}),
	)

	got := output.String()
	for _, secret := range []string{
		"direct-struct-secret",
		"pointer-struct-secret",
		"slice-struct-secret",
		"array-struct-secret",
		"map-struct-secret",
		"ignored-struct-value",
		"unexported-struct-value",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("log output exposed %q: %s", secret, got)
		}
	}
	for _, ordinary := range []string{
		"direct-struct-public",
		"pointer-struct-public",
		"slice-struct-public",
		"array-struct-public",
		"map-struct-public",
	} {
		if !strings.Contains(got, ordinary) {
			t.Fatalf("ordinary struct field %q was lost: %s", ordinary, got)
		}
	}
}

func TestRedactingHandlerPreservesNonCredentialFieldNames(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewRedactingJSONHandler(&output, nil))

	logger.Info("metrics",
		slog.Int("tokenCount", 42),
		slog.String("secretary", "Ada"),
		slog.Bool("cookieConsent", true),
		slog.String("access_token", "access-token-secret"),
		slog.String("refresh-token", "refresh-token-secret"),
		slog.String("clientSecret", "client-secret-value"),
		slog.String("Set-Cookie", "set-cookie-value"),
	)

	got := output.String()
	for _, ordinary := range []string{`"tokenCount":42`, `"secretary":"Ada"`, `"cookieConsent":true`} {
		if !strings.Contains(got, ordinary) {
			t.Fatalf("ordinary field %s was changed: %s", ordinary, got)
		}
	}
	for _, secret := range []string{"access-token-secret", "refresh-token-secret", "client-secret-value", "set-cookie-value"} {
		if strings.Contains(got, secret) {
			t.Fatalf("credential field exposed %q: %s", secret, got)
		}
	}
}

func TestRedactingHandlerHandlesStructCyclesAndSpecialValues(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewRedactingJSONHandler(&output, nil))
	timestamp := time.Date(2026, time.September, 4, 10, 11, 12, 0, time.UTC)
	metadata := &cyclicMetadata{
		Token: "cyclic-token-secret",
		At:    timestamp,
		Err:   os.ErrPermission,
	}
	metadata.Next = metadata

	logger.Info("cycle", slog.Any("metadata", metadata))

	got := output.String()
	if strings.Contains(got, "cyclic-token-secret") || strings.Contains(got, os.ErrPermission.Error()) {
		t.Fatalf("cyclic or error data leaked: %s", got)
	}
	if !strings.Contains(got, timestamp.Format(time.RFC3339)) {
		t.Fatalf("time value was not preserved: %s", got)
	}
}

func TestRedactingHandlerCoversCredentialKeyVariantsWithoutMetricFalsePositives(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewRedactingJSONHandler(&output, nil))

	logger.Info("credential keys",
		slog.String("openai_api_key", "openai-key-secret"),
		slog.String("masterPassword", "master-password-secret"),
		slog.String("encryption-key", "encryption-key-secret"),
		slog.String("private.key", "private-key-secret"),
		slog.String("databasePassword", "database-password-secret"),
		slog.String("sessionToken", "session-token-secret"),
		slog.Any("metadata", extendedSecrets{
			OpenAIAPIKey: "struct-openai-key-secret",
			Nested: map[string]any{
				"master_password": "map-master-password-secret",
				"items": []any{
					map[string]any{"private_key": "slice-private-key-secret"},
				},
			},
		}),
		slog.Int("tokenCount", 84),
		slog.String("secretary", "Grace"),
		slog.Bool("cookieConsent", true),
		slog.String("authorizationStatus", "granted"),
	)

	got := output.String()
	for _, secret := range []string{
		"openai-key-secret",
		"master-password-secret",
		"encryption-key-secret",
		"private-key-secret",
		"database-password-secret",
		"session-token-secret",
		"struct-openai-key-secret",
		"map-master-password-secret",
		"slice-private-key-secret",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("credential key variant exposed %q: %s", secret, got)
		}
	}
	for _, ordinary := range []string{`"tokenCount":84`, `"secretary":"Grace"`, `"cookieConsent":true`, `"authorizationStatus":"granted"`} {
		if !strings.Contains(got, ordinary) {
			t.Fatalf("ordinary field %s was changed: %s", ordinary, got)
		}
	}
}

func TestRedactingHandlerCleansMessagesAndStringValues(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewRedactingJSONHandler(&output, nil))

	logger.Info(
		"request failed with Bearer message-bearer-credential and sk-1234567890abcdefghijklmnop",
		slog.String("detail", "Authorization: Bearer attr-bearer-credential"),
		slog.String("url", "https://example.test/path?visible=kept&token=query-token-secret&access_token=url%2Ftoken%2Bsecret&api%5Fkey=encoded-key-secret&key=query-key-secret&secret=query-secret-value&password=password-secret#fragment"),
		slog.String("malformedURL", "https://example.test/path?bad%ZZ=malformed-query-secret"),
		slog.Any("nested", map[string]any{
			"message": []string{
				"keep this ordinary text",
				"cookie=session-cookie-secret",
			},
		}),
	)

	got := output.String()
	for _, secret := range []string{
		"message-bearer-credential",
		"sk-1234567890abcdefghijklmnop",
		"attr-bearer-credential",
		"query-token-secret",
		"url%2Ftoken%2Bsecret",
		"encoded-key-secret",
		"query-key-secret",
		"query-secret-value",
		"password-secret",
		"session-cookie-secret",
		"malformed-query-secret",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("string redaction exposed %q: %s", secret, got)
		}
	}
	for _, ordinary := range []string{"request failed with", "https://example.test/path", "visible=kept", "#fragment", "keep this ordinary text"} {
		if !strings.Contains(got, ordinary) {
			t.Fatalf("ordinary string content %q was lost: %s", ordinary, got)
		}
	}
	if strings.Count(got, Redacted) < 11 {
		t.Fatalf("expected all message and string secrets to be visibly redacted: %s", got)
	}
}

func TestRedactingHandlerCleansAuthenticationAndCookieSyntaxCompletely(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewRedactingJSONHandler(&output, nil))

	logger.Info(
		"token=bare-token-secret token: colon-token-secret\nkeep-message-line",
		slog.String("authorization", "field-name-redacts-this"),
		slog.String("basicHeader", "Authorization: Basic dXNlcjpwYXNz\nkeep-basic-line"),
		slog.String("proxyHeader", "Proxy-Authorization: Basic cHJveHk6cGFzcw==\r\nkeep-proxy-line"),
		slog.String("cookieHeader", "Cookie: sid=abc; refresh=def; theme=dark\nkeep-cookie-line"),
		slog.String("setCookieHeader", "Set-Cookie: sid=ghi; Path=/; HttpOnly; SameSite=Lax\nkeep-set-cookie-line"),
		slog.String("shortBearer", "Bearer abc123,keep-after-comma"),
		slog.String("punctuatedBearer", "Bearer abcdefgh!tail;keep-after-semicolon"),
		slog.String("quotedBearer", "Bearer \"quoted token!tail\",keep-after-quote"),
		slog.String("lineBearer", "Bearer line-token-secret\nkeep-after-newline"),
	)

	got := output.String()
	for _, secret := range []string{
		"bare-token-secret",
		"colon-token-secret",
		"field-name-redacts-this",
		"dXNlcjpwYXNz",
		"cHJveHk6cGFzcw==",
		"sid=abc",
		"refresh=def",
		"theme=dark",
		"sid=ghi",
		"Path=/",
		"HttpOnly",
		"SameSite=Lax",
		"abc123",
		"abcdefgh!tail",
		"quoted token!tail",
		"line-token-secret",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("authentication or cookie syntax exposed %q: %s", secret, got)
		}
	}
	for _, ordinary := range []string{
		"keep-message-line",
		"keep-basic-line",
		"keep-proxy-line",
		"keep-cookie-line",
		"keep-set-cookie-line",
		"keep-after-comma",
		"keep-after-semicolon",
		"keep-after-quote",
		"keep-after-newline",
	} {
		if !strings.Contains(got, ordinary) {
			t.Fatalf("safe boundary content %q was lost: %s", ordinary, got)
		}
	}
}

func TestRedactingHandlerUsesEscapeAwareCredentialScanning(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewRedactingJSONHandler(&output, nil))

	logger.Info(
		`Bearer "prefix\"remaining-secret",after-message`,
		slog.String("singleBearer", `Bearer 'single\'remaining-secret';after-single`),
		slog.String("evenBackslashBearer", `Bearer "even-backslash-secret\\",after-even-backslashes`),
		slog.String("escapedAssignment", `token="prefix\"remaining-secret";after-assignment`),
		slog.String("singleAssignment", `client_secret='single\'assignment-secret',after-client`),
		slog.String("unclosedBearer", `Bearer "unclosed-bearer-secret`),
		slog.String("unclosedAssignment", `password="unclosed-password-secret`),
		slog.String("vendorAssignments", `AZURE_OPENAI_API_KEY=opaque-secret-value session_token=session-secret-value databasePassword=database-secret-value vendor.openai-api-key=vendor-secret-value`),
		slog.String("ordinaryQuoted", `note="prefix\"ordinary";after-note`),
		slog.String("ordinaryMetrics", `tokenCount="42" authorizationStatus='granted' cookieConsent=true`),
	)

	got := output.String()
	for _, secret := range []string{
		"remaining-secret",
		"even-backslash-secret",
		"assignment-secret",
		"unclosed-bearer-secret",
		"unclosed-password-secret",
		"opaque-secret-value",
		"session-secret-value",
		"database-secret-value",
		"vendor-secret-value",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("escape-aware scan exposed %q: %s", secret, got)
		}
	}
	for _, ordinary := range []string{
		"after-message",
		"after-single",
		"after-even-backslashes",
		"after-assignment",
		"after-client",
		`note=\"prefix\\\"ordinary\";after-note`,
		`tokenCount=\"42\" authorizationStatus='granted' cookieConsent=true`,
	} {
		if !strings.Contains(got, ordinary) {
			t.Fatalf("ordinary text %q was changed: %s", ordinary, got)
		}
	}
}

func TestRedactingHandlerScansQuotedKeysAndEqualsForms(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(NewRedactingJSONHandler(&output, nil))

	logger.Info(
		`{"token":"json-token-secret","tokenCount":3}`,
		slog.String("authorizationJSON", `{"Authorization":"Basic dXNlcjpwYXNz","status":"kept"}`),
		slog.String("cookieJSON", `{"Cookie":"sid=json-cookie-secret","enabled":true}`),
		slog.String("singleQuotedConfig", `'session_token'='single-config-secret'; ordinary='kept'`),
		slog.String("escapedKey", `{"to\"ken":"ordinary-escaped-key-value","token":"escaped-boundary-secret","title":"ordinary quoted text"}`),
		slog.String("escapedSensitiveKeys", `{"api\u005fkey":"unicode-key-secret",'session\_token':'single-escaped-key-secret'}`),
		slog.String("authorizationEquals", "Authorization=Basic authorization-equals-secret\nkeep-authorization-line"),
		slog.String("proxyAuthorizationEquals", "Proxy-Authorization=Basic proxy-equals-secret\r\nkeep-proxy-line"),
		slog.String("tokenBearerEquals", `token=Bearer token-bearer-secret,after-token`),
		slog.String("cookieEquals", "Cookie=sid=cookie-one-secret; refresh=cookie-two-secret\nkeep-cookie-line"),
		slog.String("setCookieEquals", "Set-Cookie=sid=set-cookie-secret; Path=/; HttpOnly\nkeep-set-cookie-line"),
		slog.String("ordinaryJSON", `{"title":"ordinary quoted value","tokenCount":7,"authorizationStatus":"ready"}`),
	)

	got := output.String()
	for _, secret := range []string{
		"json-token-secret",
		"dXNlcjpwYXNz",
		"json-cookie-secret",
		"single-config-secret",
		"escaped-boundary-secret",
		"unicode-key-secret",
		"single-escaped-key-secret",
		"authorization-equals-secret",
		"proxy-equals-secret",
		"token-bearer-secret",
		"cookie-one-secret",
		"cookie-two-secret",
		"set-cookie-secret",
		"Path=/",
		"HttpOnly",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("quoted key or equals form exposed %q: %s", secret, got)
		}
	}
	for _, ordinary := range []string{
		`\"tokenCount\":3`,
		`\"status\":\"kept\"`,
		`\"enabled\":true`,
		`ordinary='kept'`,
		"ordinary-escaped-key-value",
		"ordinary quoted text",
		"keep-authorization-line",
		"keep-proxy-line",
		"after-token",
		"keep-cookie-line",
		"keep-set-cookie-line",
		"ordinary quoted value",
		`\"tokenCount\":7`,
		`\"authorizationStatus\":\"ready\"`,
	} {
		if !strings.Contains(got, ordinary) {
			t.Fatalf("ordinary structure or text %q was changed: %s", ordinary, got)
		}
	}
}

func TestSanitizeTextIsIdempotentAcrossRedactionStages(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "query", input: `?token=secret`, expected: `?token=[REDACTED]`},
		{name: "header", input: `Authorization: Basic secret`, expected: `Authorization:[REDACTED]`},
		{name: "bearer", input: `Bearer opaque-secret`, expected: `Bearer [REDACTED]`},
		{name: "assignment", input: `token=opaque-secret`, expected: `token=[REDACTED]`},
		{name: "known API key", input: `sk-1234567890abcdefghijklmnop`, expected: `[REDACTED]`},
		{name: "already redacted query", input: `?token=[REDACTED]`, expected: `?token=[REDACTED]`},
		{name: "already redacted header", input: `Authorization:[REDACTED]`, expected: `Authorization:[REDACTED]`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sanitizeText(test.input)
			if got != test.expected {
				t.Fatalf("sanitizeText(%q) = %q, want %q", test.input, got, test.expected)
			}
			for iteration := 0; iteration < 5; iteration++ {
				next := sanitizeText(got)
				if next != got {
					t.Fatalf("sanitize iteration %d changed %q to %q", iteration+2, got, next)
				}
				got = next
			}
			if strings.Contains(got, Redacted+"]") {
				t.Fatalf("redaction accumulated a closing bracket: %q", got)
			}
		})
	}
}

func TestSanitizeTextDecodesSingleQuotedKeysSafely(t *testing.T) {
	input := `'api\u005fkey'='unicode-secret';'api\x5fkey'='hex-secret';'api\137key'='octal-secret';'author\'name'='ordinary-single-value';'api\qkey'='invalid-escape-secret'`

	got := sanitizeText(input)
	for _, secret := range []string{"unicode-secret", "hex-secret", "octal-secret", "invalid-escape-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("single-quoted key exposed %q: %s", secret, got)
		}
	}
	for _, expected := range []string{
		`'api\u005fkey'=` + Redacted,
		`'api\x5fkey'=` + Redacted,
		`'api\137key'=` + Redacted,
		`'author\'name'='ordinary-single-value'`,
		`'api\qkey'=` + Redacted,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("single-quoted output lost %q: %s", expected, got)
		}
	}
}

func TestSanitizeTextPreservesAdjacentFieldsForQuotedHeaderKeys(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		secret   string
	}{
		{
			name:     "authorization",
			input:    `{"Authorization":"Basic authorization-secret","status":"kept"}`,
			expected: `{"Authorization":[REDACTED],"status":"kept"}`,
			secret:   "authorization-secret",
		},
		{
			name:     "proxy authorization",
			input:    `{"Proxy-Authorization":"Basic proxy-secret","status":"kept"}`,
			expected: `{"Proxy-Authorization":[REDACTED],"status":"kept"}`,
			secret:   "proxy-secret",
		},
		{
			name:     "cookie",
			input:    `{"Cookie":"sid=cookie-secret","status":"kept"}`,
			expected: `{"Cookie":[REDACTED],"status":"kept"}`,
			secret:   "cookie-secret",
		},
		{
			name:     "set cookie",
			input:    `{"Set-Cookie":"sid=set-cookie-secret; Path=/","status":"kept"}`,
			expected: `{"Set-Cookie":[REDACTED],"status":"kept"}`,
			secret:   "set-cookie-secret",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first := sanitizeText(test.input)
			if first != test.expected {
				t.Fatalf("first sanitize = %q, want %q", first, test.expected)
			}
			second := sanitizeText(first)
			if second != first {
				t.Fatalf("second sanitize changed %q to %q", first, second)
			}
			if strings.Contains(second, test.secret) || !strings.Contains(second, `"status":"kept"`) {
				t.Fatalf("credential leaked or adjacent field was lost: %q", second)
			}
		})
	}
}

func TestSanitizeTextDoesNotTrustCommaAfterUnquotedHeaderMarker(t *testing.T) {
	const input = `Authorization=[REDACTED],remaining-secret`

	got := sanitizeText(input)
	if strings.Contains(got, "remaining-secret") {
		t.Fatalf("unquoted header suffix leaked: %q", got)
	}
	if got != `Authorization=[REDACTED]` {
		t.Fatalf("sanitizeText(%q) = %q", input, got)
	}
}

func TestSanitizeTextScansOrdinaryQuotedContents(t *testing.T) {
	tests := []struct {
		name, input, expected string
	}{
		{"note", `note="token=nested-token-fixture"`, `note="token=[REDACTED]"`},
		{"JSON message", `{"message":"token=nested-token-fixture","status":"kept"}`, `{"message":"token=[REDACTED]","status":"kept"}`},
		{"single quote", `detail='master_password=nested-password-fixture';status=kept`, `detail='master_password=[REDACTED]';status=kept`},
		{"nested quotes", `note="detail='token=nested-token-fixture';status=kept"`, `note="detail='token=[REDACTED]';status=kept"`},
		{"ordinary escape", `note="prefix\"ordinary";status='kept'`, `note="prefix\"ordinary";status='kept'`},
		{"ordinary JSON", `{"message":"ordinary quoted text","tokenCount":3}`, `{"message":"ordinary quoted text","tokenCount":3}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sanitizeText(test.input)
			if got != test.expected {
				t.Fatalf("sanitizeText = %q, want %q", got, test.expected)
			}
			for iteration := 0; iteration < 5; iteration++ {
				if next := sanitizeText(got); next != got {
					t.Fatalf("sanitize iteration %d changed %q to %q", iteration+2, got, next)
				}
			}
		})
	}
}

func BenchmarkSanitizeTextQuotedContents(b *testing.B) {
	for _, test := range []struct {
		name    string
		repeats int
	}{{"small", 1024}, {"large", 16384}} {
		b.Run(test.name, func(b *testing.B) {
			input := `note="` + strings.Repeat(`detail='ordinary text;token=nested-token-fixture';`, test.repeats) + `"`
			b.SetBytes(int64(len(input)))
			b.ReportAllocs()
			for b.Loop() {
				_ = sanitizeText(input)
			}
		})
	}
}

func TestOpenCreatesPrivateAppendOnlyJSONLog(t *testing.T) {
	logs := filepath.Join(t.TempDir(), "logs")
	if err := os.Mkdir(logs, 0o700); err != nil {
		t.Fatal(err)
	}

	logger, closer, err := Open(logs)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("first", slog.String("token", "must-not-appear"))
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	logger, closer, err = Open(logs)
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("second", slog.String("status", "ready"))
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(logs, "app.jsonl")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("log file is not private: %o", info.Mode().Perm())
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	if !strings.Contains(got, `"msg":"first"`) || !strings.Contains(got, `"msg":"second"`) {
		t.Fatalf("log file was not appended: %s", got)
	}
	if strings.Contains(got, "must-not-appear") || !strings.Contains(got, Redacted) {
		t.Fatalf("file log redaction failed: %s", got)
	}
}

func TestOpenFailureReturnsSafeApplicationError(t *testing.T) {
	root := t.TempDir()
	notDirectory := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(notDirectory, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := Open(notDirectory)
	if err == nil {
		t.Fatal("Open succeeded with a file as the log directory")
	}
	if strings.Contains(err.Error(), notDirectory) {
		t.Fatal("safe error exposed an absolute path")
	}
}
