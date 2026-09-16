// Package legacy converts a legacy browser snapshot into the Go core's
// entities and imports it atomically.
//
// It owns the transformation (the frontend only extracts), the fingerprint that
// makes an import idempotent, and the report that makes every unsupported field
// visible. The rules it implements are ADR-0006's: files commit before the
// database transaction, one transaction covers the metadata, and nothing is
// dropped silently.
package legacy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Snapshot is the envelope the frontend extracts from the browser stores.
//
// Every array carries the legacy rows verbatim: transformation happens here,
// not in the webview, so the same input always produces the same output and the
// legacy shape stays auditable (ADR-0006 §1).
type Snapshot struct {
	App             string            `json:"app"`
	ManifestVersion int               `json:"manifestVersion"`
	ExportedAt      string            `json:"exportedAt"`
	Projects        []LegacyProject   `json:"projects"`
	Assets          []LegacyAsset     `json:"assets"`
	History         []LegacyHistory   `json:"generationHistory"`
	PromptLibrary   LegacyPromptData  `json:"promptLibrary"`
	UIPreferences   map[string]string `json:"uiPreferences"`
	Media           []LegacyMedia     `json:"media"`
	Unsupported     map[string]any    `json:"unsupported"`
}

// SupportedManifestVersion is the only envelope version this package accepts.
// An unknown version is refused rather than parsed optimistically, because a
// newer envelope may carry fields whose absence changes the meaning of the
// older ones.
const SupportedManifestVersion = 1

// LegacyProject mirrors the canvas store's CanvasProject.
type LegacyProject struct {
	ID             string             `json:"id"`
	Title          string             `json:"title"`
	CreatedAt      string             `json:"createdAt"`
	UpdatedAt      string             `json:"updatedAt"`
	Nodes          []LegacyNode       `json:"nodes"`
	Connections    []LegacyConnection `json:"connections"`
	ChatSessions   []LegacyChat       `json:"chatSessions"`
	ActiveChatID   string             `json:"activeChatId"`
	BackgroundMode string             `json:"backgroundMode"`
	ShowImageInfo  bool               `json:"showImageInfo"`
	Viewport       LegacyViewport     `json:"viewport"`
}

// LegacyNode mirrors the canvas store's CanvasNodeData. Metadata is kept as a
// raw map so unknown keys survive: the type's real shape is open (a plugin node
// defines its own), and decoding into a fixed struct would drop them.
type LegacyNode struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Title    string         `json:"title"`
	Position LegacyPosition `json:"position"`
	Width    float64        `json:"width"`
	Height   float64        `json:"height"`
	Metadata map[string]any `json:"metadata"`
}

// LegacyPosition is a node's top-left corner.
type LegacyPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// LegacyConnection is the legacy edge: id, from and to, with no relation type.
type LegacyConnection struct {
	ID         string         `json:"id"`
	FromNodeID string         `json:"fromNodeId"`
	ToNodeID   string         `json:"toNodeId"`
	Extra      map[string]any `json:"-"`
}

// UnmarshalJSON keeps the connection's unknown keys.
//
// encoding/json cannot "keep the rest" with a struct, so the raw object is
// decoded once, the known keys are read, and whatever remains is stored. Those
// remaining keys become the edge's legacy metadata.
func (c *LegacyConnection) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := unmarshalObject(data, &raw); err != nil {
		return err
	}
	c.ID = stringField(raw, "id")
	c.FromNodeID = stringField(raw, "fromNodeId")
	c.ToNodeID = stringField(raw, "toNodeId")
	extra := map[string]any{}
	for key, value := range raw {
		switch key {
		case "id", "fromNodeId", "toNodeId":
			continue
		default:
			extra[key] = value
		}
	}
	if len(extra) > 0 {
		c.Extra = extra
	}
	return nil
}

// LegacyChat mirrors the canvas store's CanvasAssistantSession.
type LegacyChat struct {
	ID        string           `json:"id"`
	Title     string           `json:"title"`
	CreatedAt string           `json:"createdAt"`
	UpdatedAt string           `json:"updatedAt"`
	Messages  []map[string]any `json:"messages"`
}

// LegacyViewport is the canvas transform.
type LegacyViewport struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	K float64 `json:"k"`
}

// LegacyAsset mirrors the asset store's Asset.
type LegacyAsset struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Title     string         `json:"title"`
	CoverURL  string         `json:"coverUrl"`
	Tags      []string       `json:"tags"`
	Source    string         `json:"source"`
	Note      string         `json:"note"`
	CreatedAt string         `json:"createdAt"`
	UpdatedAt string         `json:"updatedAt"`
	Data      map[string]any `json:"data"`
}

// LegacyHistory mirrors the generation history store's record.
type LegacyHistory struct {
	ID           string              `json:"id"`
	CreatedAt    int64               `json:"createdAt"`
	Prompt       string              `json:"prompt"`
	Model        string              `json:"model"`
	Images       []LegacyHistoryItem `json:"images"`
	SuccessCount int                 `json:"successCount"`
	FailCount    int                 `json:"failCount"`
}

// LegacyHistoryItem is one image a history record refers to.
type LegacyHistoryItem struct {
	StorageKey string `json:"storageKey"`
	MIMEType   string `json:"mimeType"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Bytes      int64  `json:"bytes"`
}

// LegacyPromptData is the prompt library's persisted state. WP-04 does not move
// it into the domain; it is recorded so the report can say it was seen.
type LegacyPromptData struct {
	BuiltInCovers map[string]string `json:"builtInCovers"`
}

// LegacyMedia is one blob the frontend will upload separately.
type LegacyMedia struct {
	LegacyKey string `json:"legacyKey"`
	MIMEType  string `json:"mimeType"`
	Bytes     int64  `json:"bytes"`
	// File names the fixture file, for tests. A real extraction leaves it empty
	// and streams the blob through the chunked upload path instead.
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

// Fingerprint derives the content fingerprint of one project.
//
// It covers the identity and the shape a user would notice: the legacy id, the
// title, the node and edge counts, and the sorted media hashes the project's
// nodes reference. Two imports of the same project therefore collide, which is
// what AC-LEGACY-002's "second import detects it" needs, while an edited
// project does not (importing an edited legacy project is a new import, which
// is the honest outcome: the store it came from is the same but its content is
// not).
func Fingerprint(project LegacyProject, mediaHashes map[string]string) string {
	hasher := sha256.New()
	hasher.Write([]byte(project.ID))
	hasher.Write([]byte{0})
	hasher.Write([]byte(project.Title))
	hasher.Write([]byte{0})
	hasher.Write([]byte(strconv.Itoa(len(project.Nodes))))
	hasher.Write([]byte{0})
	hasher.Write([]byte(strconv.Itoa(len(project.Connections))))

	// Collect the media keys the project's nodes and assets reference, resolve
	// them to hashes, and fold in the sorted result. Sorting makes the value
	// independent of node order, so reordering nodes on the canvas does not look
	// like a different project.
	keys := referencedMediaKeys(project)
	sort.Strings(keys)
	for _, key := range keys {
		hasher.Write([]byte{0})
		hasher.Write([]byte(key))
		hasher.Write([]byte{0})
		hasher.Write([]byte(mediaHashes[key]))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// SnapshotFingerprint derives the fingerprint of a whole snapshot, for the
// run-level record.
func SnapshotFingerprint(snapshot Snapshot, mediaHashes map[string]string) string {
	hasher := sha256.New()
	hasher.Write([]byte(strconv.Itoa(len(snapshot.Projects))))
	hasher.Write([]byte{0})
	hasher.Write([]byte(strconv.Itoa(len(snapshot.Assets))))
	for _, project := range snapshot.Projects {
		hasher.Write([]byte{0})
		hasher.Write([]byte(Fingerprint(project, mediaHashes)))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// referencedMediaKeys collects every legacy storage key a project's nodes
// mention, in any of the places the legacy shape keeps one.
func referencedMediaKeys(project LegacyProject) []string {
	seen := map[string]bool{}
	collect := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || strings.HasPrefix(trimmed, "data:") ||
			strings.HasPrefix(trimmed, "blob:") || strings.HasPrefix(trimmed, "http") {
			// A data URL, an object URL and a remote URL are not FileStore keys.
			return
		}
		seen[trimmed] = true
	}
	for _, node := range project.Nodes {
		for _, key := range []string{"content", "storageKey", "coverUrl"} {
			if value, ok := node.Metadata[key].(string); ok {
				collect(value)
			}
		}
		if references, ok := node.Metadata["references"].([]any); ok {
			for _, reference := range references {
				if value, ok := reference.(string); ok {
					collect(value)
				}
			}
		}
		if images, ok := node.Metadata["images"].([]any); ok {
			for _, image := range images {
				if object, ok := image.(map[string]any); ok {
					if value, ok := object["storageKey"].(string); ok {
						collect(value)
					}
					if value, ok := object["content"].(string); ok {
						collect(value)
					}
				}
			}
		}
	}
	for _, session := range project.ChatSessions {
		for _, message := range session.Messages {
			references, ok := message["references"].([]any)
			if !ok {
				continue
			}
			for _, reference := range references {
				if object, ok := reference.(map[string]any); ok {
					if value, ok := object["storageKey"].(string); ok {
						collect(value)
					}
				}
			}
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	return keys
}

// isMediaKey reports whether a legacy string names a stored blob rather than an
// inline data URL or a remote address.
func isMediaKey(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	for _, prefix := range []string{"data:", "blob:", "http:", "https:"} {
		if strings.HasPrefix(trimmed, prefix) {
			return false
		}
	}
	return true
}

// describe renders a value for a warning detail without ever echoing a path or
// a secret: only a short, printable summary.
func describe(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		if len(typed) > 40 {
			return fmt.Sprintf("string(%d)", len(typed))
		}
		return "string"
	case float64, bool:
		return fmt.Sprintf("%v", typed)
	case []any:
		return fmt.Sprintf("list(%d)", len(typed))
	case map[string]any:
		return fmt.Sprintf("object(%d)", len(typed))
	default:
		return "value"
	}
}

// unmarshalObject decodes a JSON object into a map, used by the connection
// custom unmarshaller.
func unmarshalObject(data []byte, target *map[string]any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(target)
}

// stringField reads a string field from a decoded object, tolerating a missing
// or non-string value by returning the empty string. A legacy row with a
// malformed field is a warning case, not a fatal parse error: the import must
// still carry everything else.
func stringField(object map[string]any, key string) string {
	value, ok := object[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}
