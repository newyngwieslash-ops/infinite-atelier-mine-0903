package apperror

import (
	"errors"
	"regexp"
	"testing"
)

func TestNewCreatesSafeWrappedError(t *testing.T) {
	cause := errors.New(`open C:\Users\Alice\secret.db: access denied`)
	err := New("STORAGE_OPEN_FAILED", "storage", false, "Local storage could not be opened.", cause)

	if err.Error() != "Local storage could not be opened." {
		t.Fatalf("Error() = %q", err.Error())
	}
	if !errors.Is(err, cause) {
		t.Fatal("wrapped cause is not discoverable with errors.Is")
	}
	var appErr *Error
	if !errors.As(err, &appErr) {
		t.Fatal("error is not discoverable with errors.As")
	}
	if appErr.Code != "STORAGE_OPEN_FAILED" || appErr.Category != "storage" || appErr.Retriable {
		t.Fatalf("unexpected stable error fields: %#v", appErr)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(appErr.Diagnostic) {
		t.Fatalf("diagnostic is not 16 random bytes encoded as hex: %q", appErr.Diagnostic)
	}
	if appErr.SafeMessage == cause.Error() {
		t.Fatal("safe message exposed the underlying cause")
	}
}

func TestNewCreatesDistinctDiagnostics(t *testing.T) {
	first := New("INTERNAL", "internal", false, "An internal error occurred.", nil)
	second := New("INTERNAL", "internal", false, "An internal error occurred.", nil)
	if first.Diagnostic == second.Diagnostic {
		t.Fatalf("diagnostics unexpectedly matched: %q", first.Diagnostic)
	}
}
