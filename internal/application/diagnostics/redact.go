package appdiagnostics

import (
	"regexp"
	"strings"
)

// redact.go is SECURITY section 14.1's SECOND layer.
//
// # Why there are two
//
// Section 14.1 says redaction runs 「在格式化前和导出前双层执行」 — before formatting AND before
// export. The first layer is `logging.NewRedactingJSONHandler`, which masks a value as it is written
// to `app.jsonl`. This is the second, and it is not redundancy for its own sake:
//
//   - the log file may have been written by an OLDER build whose handler masked less;
//   - it may have been appended to by hand, or restored from a backup taken before the handler
//     existed;
//   - the file is on disk, where anything with access can put something into it.
//
// A bundle is the one artifact a user SENDS TO SOMEBODY ELSE, so a secret that got past the first
// layer leaves the machine rather than staying on it. The second pass is what makes that difference.
//
// # What it masks, and the two things it deliberately does not
//
// The patterns are the shapes section 14.1 names: Authorization headers, Bearer values, the common
// API-key prefixes, cookies, query tokens and `secret`-named assignments. Each is masked by
// REPLACING the value and KEEPING the name, because `Authorization: [REDACTED]` tells a reader what
// happened while `[REDACTED]` alone looks like corruption.
//
// It does NOT try to detect story text. A bundle's log entries are messages and codes rather than
// content — that is the log handler's own rule — and a pattern that guessed at prose would mask a
// user's own words in a file whose purpose is diagnosis.
//
// It does NOT mask file paths: a path is what a support conversation needs, and section 14.1 makes
// anonymising them OPTIONAL (「本地用户路径可选择匿名化」) rather than required. The choice belongs to
// a UI with a checkbox, not to a regex.

// Redacted is what every masked value becomes. It matches the log handler's own marker so a reader
// sees one convention rather than two.
const Redacted = "[REDACTED]"

var (
	// headerPattern masks a credential header's VALUE and keeps its name.
	headerPattern = regexp.MustCompile(`(?im)\b(Proxy-Authorization|Authorization|Set-Cookie|Cookie|X-Api-Key)[\t ]*:[^\r\n]*`)

	// bearerPattern masks what follows the word Bearer.
	bearerPattern = regexp.MustCompile(`(?i)\bBearer[\t ]+[A-Za-z0-9._~+/=-]{8,}`)

	// apiKeyPatterns are the four shapes section 14.1 names, as the log handler already defines them.
	// They are repeated here rather than exported from the infrastructure package because the
	// application layer may not import infrastructure (AGENTS section 7.2) — and a shared constant
	// across that boundary would invert the dependency.
	apiKeyPatterns = []*regexp.Regexp{
		regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`),
		regexp.MustCompile(`AIza[0-9A-Za-z_-]{20,}`),
		regexp.MustCompile(`gh[pousr]_[0-9A-Za-z]{20,}`),
		regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	}

	// assignmentPattern masks `secret=...`, `api_key: ...` and their siblings, including quoted keys.
	assignmentPattern = regexp.MustCompile(`(?i)\b(api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret|password|secret|token|private[_-]?key)\b["']?[\t ]*[:=][\t ]*["']?[^\s"',;]{6,}`)

	// queryPattern masks a sensitive query parameter's value and keeps the parameter's name.
	queryPattern = regexp.MustCompile(`(?i)([?&](?:api[_-]?key|key|access[_-]?token|token|secret|password)=)[^&\s"']+`)
)

// RedactText masks the credential shapes SECURITY 14.1 lists.
//
// It is exported because the SECOND layer's caller is not only this package: a future export path
// that writes a user's own text should run the same function rather than growing a third set of
// patterns. One definition is what keeps "the export is redacted" a property rather than a claim.
func RedactText(text string) string {
	if text == "" {
		return text
	}
	// Headers first: masking `Authorization: ...` before the assignment pass stops that pass from
	// reading the header's own name as a key and leaving a half-masked line behind.
	text = headerPattern.ReplaceAllStringFunc(text, func(match string) string {
		if index := strings.IndexAny(match, ":="); index >= 0 {
			return match[:index+1] + " " + Redacted
		}
		return Redacted
	})
	text = bearerPattern.ReplaceAllString(text, "Bearer "+Redacted)
	for _, pattern := range apiKeyPatterns {
		text = pattern.ReplaceAllString(text, Redacted)
	}
	text = queryPattern.ReplaceAllString(text, "$1"+Redacted)
	// Assignments last, so a key name inside a query string is already masked and cannot confuse the
	// scanner's word boundaries.
	text = assignmentPattern.ReplaceAllStringFunc(text, func(match string) string {
		if index := strings.IndexAny(match, ":="); index >= 0 {
			return match[:index+1] + " " + Redacted
		}
		return Redacted
	})
	return text
}

// ContainsCredentialShape reports whether text carries a credential VALUE.
//
// It exists so a caller can REFUSE a bundle rather than shipping a masked one: masking is the right
// answer for a log line a user reads, and a refusal is the right answer for a file whose presence of
// a credential means something else went wrong. The distinction is the same one `backup`'s
// `scanArchiveForSecretShapes` makes, and this is its diagnostics-side twin.
//
// # Why it REDACTES FIRST rather than testing the raw text
//
// The first version tested the raw text against every pattern, and a test caught what that does: the
// header pattern matches `Authorization:` by NAME, so the already-redacted line
// `Authorization: [REDACTED]` — exactly what the redactor produces — reported a credential and the
// caller would refuse a clean bundle. The shapes that matter are the VALUES, so this runs the
// redactor and asks whether it CHANGED anything: a text the redactor leaves alone carries no value to
// mask, and a text it changes carried one.
//
// That is also the honest reading of the function's own name. The caller's question is not "does this
// line mention a credential" — a log line saying `api_key` is ordinary — it is "is there something
// here that must not leave the machine".
func ContainsCredentialShape(text string) bool {
	return RedactText(text) != text
}
