// Package schemas embeds the machine-readable contracts in this directory.
//
// The files are the contracts; this package only carries them into the binary.
// They are embedded rather than read from disk because a packaged desktop build
// has no repository tree beside it — a runtime path lookup would work in
// development and fail in the installer, which is the worst time to find out.
//
// docs/ARCHITECTURE.md section 5 places `schemas/agent` beside the repository
// root, and AGENT_CONTRACTS section 4.2 refers to a schema by its path, so Lookup
// resolves those same paths. A skill manifest can therefore name
// `schemas/agent/event_extraction.v1.json` and get the same bytes this package
// validates with, with no second copy to keep in step.
package schemas

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sync"
)

// files holds every schema in this directory tree.
//
//go:embed agent/*.json agent/tools/*.json
var files embed.FS

// AgentEventExtractionPath is the path AGENT_CONTRACTS section 4.2's manifest
// form uses to name this schema.
const AgentEventExtractionPath = "schemas/agent/event_extraction.v1.json"

// The rest of the agent contract, by the path a skill manifest names them with.
//
// They are constants rather than string literals at the call sites so a rename
// breaks the build instead of producing a "no schema is embedded at ..." error at
// runtime, and so the set of contracts this build carries is greppable.
const (
	AgentDecisionRequestPath  = "schemas/agent/decision-request.v1.json"
	AgentDecisionResultPath   = "schemas/agent/decision-result.v1.json"
	AgentExecutionRequestPath = "schemas/agent/execution-request.v1.json"
	AgentExecutionResultPath  = "schemas/agent/execution-result.v1.json"
	AgentSupervisionReqPath   = "schemas/agent/supervision-request.v1.json"
	AgentReviewReportPath     = "schemas/agent/review-report.v1.json"
	AgentErrorPath            = "schemas/agent/agent-error.v1.json"
)

// embeddedPath is the same file as this package sees it: embed paths are
// relative to the directory holding the directive, with no leading `schemas/`.
//
// It is unused today because Lookup addresses files by their specification path,
// but it documents the translation that path.Join("schemas", dir) performs, which
// is what a reader checking the embed directive against the constants needs.
const embeddedEventExtractionPath = "agent/event_extraction.v1.json"

// AgentPaths lists every agent-contract schema this build embeds, in the order the
// agent runtime compiles them. It exists so the runtime's registry and the tests
// can iterate the whole set rather than naming files one at a time: a schema added
// to the directory but forgotten in a call site is then impossible.
var AgentPaths = []string{
	AgentDecisionRequestPath,
	AgentDecisionResultPath,
	AgentExecutionRequestPath,
	AgentExecutionResultPath,
	AgentSupervisionReqPath,
	AgentReviewReportPath,
	AgentErrorPath,
	AgentEventExtractionPath,
}

// cached schemas, read once. A schema is immutable at runtime, so re-reading it
// per validation would only add a failure mode.
var (
	once     sync.Once
	contents map[string][]byte
	readErr  error
)

// load reads every embedded schema exactly once.
func load() (map[string][]byte, error) {
	once.Do(func() {
		contents = map[string][]byte{}
		readErr = fs.WalkDir(files, ".", func(dir string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			// A config-relative name, so a caller can compare it with the path a
			// manifest uses by trimming the leading `schemas/`.
			name := path.Join("schemas", dir)
			body, err := files.ReadFile(dir)
			if err != nil {
				return err
			}
			contents[name] = body
			return nil
		})
	})
	return contents, readErr
}

// Lookup returns a schema's bytes by the path the specification uses, such as
// AgentEventExtractionPath.
//
// An unknown path is an error rather than an empty document: a caller asking for
// a schema that does not exist has a bug, and handing it nothing would surface as
// a confusing validation failure instead.
func Lookup(name string) ([]byte, error) {
	all, err := load()
	if err != nil {
		return nil, fmt.Errorf("reading the embedded schemas: %w", err)
	}
	body, ok := all[name]
	if !ok {
		return nil, fmt.Errorf("no schema is embedded at %s", name)
	}
	// A copy, so a caller cannot mutate the cached bytes for everyone else.
	return append([]byte(nil), body...), nil
}

// AgentEventExtraction returns the event extraction contract.
func AgentEventExtraction() ([]byte, error) {
	return Lookup(AgentEventExtractionPath)
}
