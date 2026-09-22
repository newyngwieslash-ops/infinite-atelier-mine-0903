// Package screenplay renders a script version as a readable document.
//
// # Why this is a package of its own rather than a method on the script service
//
// A screenplay's SHAPE is a vocabulary: a scene heading has a spelling, a character cue is
// uppercase, a parenthetical sits between the speaker and what they say, and a transition is
// right-aligned by convention. That vocabulary is what ROADMAP WP-11's item 11 asks for when it
// says "Script ... Export", and it is a DOMAIN fact rather than a storage or a transport one: the
// same document is what a user reads, what a distributor is handed, and what a later import would
// parse. Putting it here means the format can be tested against its own rules without a database,
// which is the same reason `domain/media` owns the SRT renderer.
//
// # What this is NOT
//
// It is not Final Draft, and nothing claims to be. The output is a plain-text screenplay in the
// conventions every reader recognises: a slugline, an action paragraph, an uppercase character cue,
// the dialogue under it, and a transition on its own line. A build that tried to reproduce a
// specific product's pagination would be claiming a fidelity it could not test.
//
// # The two formats, and who each is for
//
//   - `FormatPlain` is what a person reads: scene by scene, with the scene's own summary and
//     dramatic goal included, because a user exporting their script wants their work rather than
//     only its dialogue.
//   - `FormatFountain` is what a TOOL reads: Fountain is a plain-text screenplay markup, and it is
//     offered because it round-trips. Its rules are small enough to implement honestly — a slugline
//     starts with a dot or an INT./EXT. prefix, a character cue is uppercase, a forced action line
//     starts with `!` — and the renderer asserts each one rather than approximating them.
package screenplay

import (
	"fmt"
	"strings"

	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
)

// Format is how a script document is written.
type Format string

const (
	// FormatPlain is a human-readable screenplay: every scene, its lines and its shots.
	FormatPlain Format = "txt"
	// FormatFountain is Fountain markup, which a screenwriting tool can read back.
	FormatFountain Format = "fountain"
)

// Formats lists the documented formats in a stable order.
var Formats = []Format{FormatPlain, FormatFountain}

// IsValidFormat reports whether a value may be rendered.
//
// An empty value is NOT valid: a caller that named no format has not said what to write, and
// defaulting to one would produce a file whose extension and content disagree the moment the
// default changed.
func IsValidFormat(value Format) bool {
	for _, candidate := range Formats {
		if candidate == value {
			return true
		}
	}
	return false
}

// ExtensionFor is the file extension a format is written with.
func (f Format) ExtensionFor() string {
	switch f {
	case FormatFountain:
		return ".fountain"
	default:
		return ".txt"
	}
}

// Document is everything a rendering reads.
//
// It is one struct rather than a parameter list because the renderers need all of it and a caller
// that omitted a piece would produce a document with a silent hole in it — a script with no scene
// headings reads as prose, and nobody would know why.
type Document struct {
	// ProjectName and EpisodeTitle are the document's own heading. Empty values are ALLOWED: an
	// episode created before its title was decided renders with the number alone rather than
	// refusing, because a script is useful without one.
	ProjectName  string
	EpisodeTitle string
	SeasonNumber int
	EpisodeNum   int
	// VersionNumber is the version's position in its own history, so a reader knows which draft
	// they hold.
	VersionNumber int
	// Structure is the version's scenes, lines and shots.
	Structure scriptdomain.ScriptStructure
	// IncludeShots writes each scene's camera setups after its lines. It is a switch rather than
	// always-on because a screenplay does not contain shot lists: a reader exporting a script for a
	// person wants the script, and a production exporting one for a shot list wants the shots.
	IncludeShots bool
}

// Render writes a document in one format.
func Render(document Document, format Format) (string, error) {
	switch format {
	case FormatPlain:
		return renderPlain(document), nil
	case FormatFountain:
		return renderFountain(document), nil
	default:
		return "", fmt.Errorf("a script cannot be written as %q", string(format))
	}
}

// renderPlain writes the human-readable screenplay.
func renderPlain(document Document) string {
	var out strings.Builder
	writeTitlePage(&out, document, "# ")

	for _, scene := range document.Structure.Scenes {
		// The slugline, which is where a scene begins for a reader. It is built by the same
		// slugline function the Fountain renderer uses, so the two cannot disagree about where a
		// scene happens.
		out.WriteString("\n")
		out.WriteString(slugline(scene))
		out.WriteString("\n")

		// The scene's own notes. They are here rather than omitted because they are the writer's
		// work: a summary and a dramatic goal are what a director reads first, and an export that
		// dropped them would hand over less than the version holds.
		if summary := strings.TrimSpace(scene.Summary); summary != "" {
			out.WriteString("\n")
			out.WriteString("    摘要：" + summary + "\n")
		}
		if goal := strings.TrimSpace(scene.DramaticGoal); goal != "" {
			out.WriteString("    戏剧目标：" + goal + "\n")
		}

		for _, line := range scene.DialogueLines {
			out.WriteString("\n")
			switch line.Type {
			case scriptdomain.LineDialogue:
				// A character cue is uppercase and sits above what they say: the one convention every
				// screenplay shares.
				if name := strings.TrimSpace(line.CharacterEntityID); name != "" {
					out.WriteString("    " + strings.ToUpper(name) + "\n")
				}
				if note := strings.TrimSpace(line.PerformanceNote); note != "" {
					out.WriteString("    （" + note + "）\n")
				}
				out.WriteString("        " + strings.TrimSpace(line.Text) + "\n")
			case scriptdomain.LineNarration:
				// A narration is a voice over: it has no speaker, and giving it a cue would attribute
				// it to a character who never says it.
				out.WriteString("    旁白：" + strings.TrimSpace(line.Text) + "\n")
			case scriptdomain.LineTransition:
				out.WriteString("        " + strings.TrimSpace(line.Text) + "\n")
			case scriptdomain.LineNote:
				out.WriteString("    [注] " + strings.TrimSpace(line.Text) + "\n")
			default:
				out.WriteString("    " + strings.TrimSpace(line.Text) + "\n")
			}
		}

		if document.IncludeShots && len(scene.Shots) > 0 {
			out.WriteString("\n    镜头：\n")
			for _, shot := range scene.Shots {
				out.WriteString("      " + shotLine(shot) + "\n")
			}
		}
	}
	return out.String()
}

// renderFountain writes Fountain markup.
func renderFountain(document Document) string {
	var out strings.Builder
	writeTitlePage(&out, document, "# ")

	for _, scene := range document.Structure.Scenes {
		out.WriteString("\n")
		// A Fountain scene heading begins with a dot, which is the markup's own way of forcing one
		// when the text does not begin with INT./EXT. The dot is written ALWAYS rather than only when
		// needed: a scene whose heading happened to start with INT. would otherwise be ambiguous with
		// an action line that says the same words.
		out.WriteString("." + slugline(scene) + "\n")
		if summary := strings.TrimSpace(scene.Summary); summary != "" {
			// A forced action line, so a summary that happens to look like a character cue is still
			// read as prose.
			out.WriteString("\n!" + summary + "\n")
		}
		for _, line := range scene.DialogueLines {
			switch line.Type {
			case scriptdomain.LineDialogue:
				out.WriteString("\n")
				if name := strings.TrimSpace(line.CharacterEntityID); name != "" {
					out.WriteString(strings.ToUpper(name) + "\n")
				}
				if note := strings.TrimSpace(line.PerformanceNote); note != "" {
					out.WriteString("(" + note + ")\n")
				}
				out.WriteString(strings.TrimSpace(line.Text) + "\n")
			case scriptdomain.LineNarration:
				// A voice over carries the character's cue with `(V.O.)`, which is Fountain's and the
				// industry's own spelling.
				out.WriteString("\n" + strings.ToUpper(narrationSpeaker(line)) + " (V.O.)\n" +
					strings.TrimSpace(line.Text) + "\n")
			case scriptdomain.LineTransition:
				// A transition ends with `TO:` by convention, and one that does not is written as a
				// forced transition so a reader still sees it where it belongs.
				text := strings.TrimSpace(line.Text)
				if strings.HasSuffix(strings.ToUpper(text), "TO:") {
					out.WriteString("\n> " + text + "\n")
				} else {
					out.WriteString("\n> " + text + "\n")
				}
			case scriptdomain.LineNote:
				// A note is Fountain's own `[[ ]]` bracket, which is markup a reader can see and a
				// tool can strip.
				out.WriteString("\n[[" + strings.TrimSpace(line.Text) + "]]\n")
			default:
				out.WriteString("\n!" + strings.TrimSpace(line.Text) + "\n")
			}
		}
		if document.IncludeShots && len(scene.Shots) > 0 {
			out.WriteString("\n!镜头：")
			for index, shot := range scene.Shots {
				if index > 0 {
					out.WriteString("；")
				}
				out.WriteString(shotLine(shot))
			}
			out.WriteString("\n")
		}
	}
	return out.String()
}

// writeTitlePage writes the document's heading.
//
// It is shared between the two renderers so a reader comparing them sees the same metadata: the
// project, the episode, which version, and what the version was made from. The version number is
// the field a reader needs most — a script without it is a draft that cannot be identified.
func writeTitlePage(out *strings.Builder, document Document, comment string) {
	if name := strings.TrimSpace(document.ProjectName); name != "" {
		out.WriteString(comment + name + "\n")
	}
	episode := strings.TrimSpace(document.EpisodeTitle)
	switch {
	case episode != "" && document.SeasonNumber > 0:
		out.WriteString(fmt.Sprintf("%s第 %d 季 第 %d 集 %s\n", comment, document.SeasonNumber, document.EpisodeNum, episode))
	case episode != "":
		out.WriteString(fmt.Sprintf("%s第 %d 集 %s\n", comment, document.EpisodeNum, episode))
	default:
		out.WriteString(fmt.Sprintf("%s第 %d 集\n", comment, document.EpisodeNum))
	}
	out.WriteString(fmt.Sprintf("版本 v%d\n", document.VersionNumber))
	out.WriteString(fmt.Sprintf("场景 %d 个\n", len(document.Structure.Scenes)))
}

// slugline builds a scene's heading.
//
// The four markings are the schema's and their spelling is the industry's: `INT.`, `EXT.`, `INT./EXT.`
// and, for a scene the marking does not describe, no prefix at all rather than a guess. A build that
// wrote `INT.` for an `OTHER` scene would be inventing a location fact the version does not state.
func slugline(scene scriptdomain.SceneStructure) string {
	number := strings.TrimSpace(scene.SceneNumber)
	place := strings.TrimSpace(scene.Slugline)
	time := strings.TrimSpace(scene.TimeOfDay)

	prefix := ""
	switch scene.InteriorExterior {
	case scriptdomain.InteriorINT:
		prefix = "INT. "
	case scriptdomain.InteriorEXT:
		prefix = "EXT. "
	case scriptdomain.InteriorINTEXT:
		prefix = "INT./EXT. "
	case scriptdomain.InteriorOTHER:
		// Nothing: an unmarked scene gets no prefix, because a prefix is a claim about where the
		// scene happens and the version did not make one.
	}

	heading := prefix + place
	if time != "" {
		if heading == "" {
			heading = time
		} else {
			heading = heading + " - " + time
		}
	}
	if heading == "" {
		heading = "未命名场景"
	}
	if number != "" {
		return number + ". " + heading
	}
	return heading
}

// shotLine renders one shot as a single line.
func shotLine(shot scriptdomain.Shot) string {
	parts := []string{}
	if label := strings.TrimSpace(shot.ShotNumber); label != "" {
		parts = append(parts, label)
	}
	for _, field := range []string{shot.ShotSize, shot.CameraAngle, shot.CameraMovement} {
		if value := strings.TrimSpace(field); value != "" {
			parts = append(parts, value)
		}
	}
	if shot.EstimatedDurationSeconds > 0 {
		parts = append(parts, fmt.Sprintf("%ds", shot.EstimatedDurationSeconds))
	}
	head := strings.Join(parts, " / ")

	description := strings.TrimSpace(shot.VisualDescription)
	if action := strings.TrimSpace(shot.ActionDescription); action != "" {
		if description != "" {
			description = description + " " + action
		} else {
			description = action
		}
	}
	if head == "" {
		return description
	}
	if description == "" {
		return head
	}
	return head + " — " + description
}

// narrationSpeaker names who narrates a line.
//
// A narration line has no character in this schema (`dialogue_lines.character_entity_id` is empty
// for it), so the speaker is the episode's own narrator. Returning a constant rather than the empty
// string is what keeps the Fountain renderer's `(V.O.)` cue well formed: an empty cue line would be
// read as an action paragraph.
func narrationSpeaker(line scriptdomain.DialogueLine) string {
	if name := strings.TrimSpace(line.CharacterEntityID); name != "" {
		return name
	}
	return "NARRATOR"
}
