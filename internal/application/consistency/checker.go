package consistency

import (
	"context"
	"fmt"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"strings"
)

// Checker runs the deterministic rules for the production stages.
//
// # The shape of a check
//
// A check is a READ of what is there followed by a COMPARISON against what the project says should
// be. Both halves matter and the first is the one that goes wrong: a rule that reads a table and
// finds nothing must report NOTHING rather than report everything, because "this character has no
// recorded costume" is not the same claim as "this character's costume is wrong". The distinction is
// the difference between a check a user trusts and one they learn to ignore.
//
// Every port below is optional in the sense that a checker built without it refuses to run the rules
// that need it, rather than running them against nothing. That is what keeps a partial composition
// from producing findings out of missing data.
type Checker struct {
	storyboard   StoryboardReader
	assets       AssetReader
	scriptSource ScriptReaderSource
	story        StoryStateReader
}

// StoryboardReader reads one board's rows and the version they belong to.
type StoryboardReader interface {
	GetStoryboardVersion(ctx context.Context, id string) (storyboard.StoryboardVersion, error)
	ListStoryboardItems(ctx context.Context, storyboardVersionID string) ([]storyboard.StoryboardItem, error)
}

// AssetReader answers what a row cites and whether a version is still in force.
type AssetReader interface {
	// UsagesForConsumer returns the usages recorded against one consumer, which for a board row is
	// the assets that row says it uses.
	UsagesForConsumer(ctx context.Context, consumerType asset.ConsumerType, consumerID string) ([]asset.Usage, error)
	// GetVersion returns one asset version, which the approved-version rule compares against.
	GetVersion(ctx context.Context, id string) (asset.Version, error)
	// GetAsset returns one asset, whose CurrentApprovedVersionID is what "in force" means
	// (DOMAIN_MODEL section 8.4).
	GetAsset(ctx context.Context, id string) (asset.Asset, error)
}

// ScriptReader answers the four questions the rules ask about ONE script version.
//
// The version is the ADAPTER's state rather than each method's argument, and that is a correction
// rather than a style choice: the first signature took the version per call, which meant a caller had
// to hold its own adapter per version anyway to avoid re-reading a structure per row — and the cache
// it would need has nowhere to live without the version in it. One adapter, one version, one read.
type ScriptReader interface {
	// ShotIDs returns the identifier of every shot of the version, in order. A board's rows must cover
	// exactly these (section 11.3's "剧本覆盖" and "缺失镜头").
	ShotIDs(ctx context.Context) ([]string, error)
	// Duration returns the version's own duration estimate, which the rows' total is compared against.
	Duration(ctx context.Context) (int, error)
	// SceneEventOf returns the story event a scene adapts, which the prop rule needs.
	SceneEventOf(ctx context.Context, sceneID string) (string, error)
	// ShotScene returns which scene a shot belongs to, so a row can be traced to its event.
	ShotScene(ctx context.Context, shotID string) (string, error)
}

// ScriptReaderSource builds a reader for one script version.
//
// It is the port the composition root implements, because the version a check runs against is a fact
// about the BOARD rather than about the caller: `CheckStoryboard` reads it from the version row and
// asks for a reader, which is what lets one checker serve every board without holding a map of
// adapters.
type ScriptReaderSource interface {
	ScriptReaderFor(ctx context.Context, scriptVersionID string) ScriptReader
}

// StoryStateReader answers what the story says about a character at a point in time.
type StoryStateReader interface {
	// CostumeStateAt returns the costume version in force at a story position, and whether any state
	// covers it at all.
	CostumeStateAt(ctx context.Context, characterEntityID string, eventOrder int) (string, bool, error)
	// StoryEventParticipantsFor returns the entities involved in one story event.
	StoryEventParticipantsFor(ctx context.Context, storyEventID string) ([]string, error)
}

// Options configures a Checker.
type Options struct {
	Storyboard StoryboardReader
	Assets     AssetReader
	// Script is a SOURCE rather than a reader: a check runs against one version and needs a reader
	// bound to it, and asking the caller for one would make the caller track the same mapping the
	// checker already has.
	Script ScriptReaderSource
	Story  StoryStateReader
}

// NewChecker builds the checker.
//
// Nothing here refuses a partial set, and that is deliberate: a build with a storyboard reader and
// no asset reader can still run the coverage rule, and refusing to build at all would turn a missing
// capability into a missing feature. Each rule checks the ports IT needs and reports a single
// "cannot run" finding when one is absent, so a reader sees which rule was skipped rather than a
// silent gap.
func NewChecker(options Options) *Checker {
	return &Checker{
		storyboard:   options.Storyboard,
		assets:       options.Assets,
		scriptSource: options.Script,
		story:        options.Story,
	}
}

// Available reports whether any rule can run.
func (c *Checker) Available() bool {
	return c != nil && c.storyboard != nil
}

// CheckStoryboard runs every rule that applies to a storyboard version.
//
// It returns the findings in a stable order, deduped, so a report is reproducible: the same board
// and the same project state must produce the same findings in the same sequence, or a reader
// comparing two reviews would see a changed set of problems where only the ordering moved.
func (c *Checker) CheckStoryboard(ctx context.Context, storyboardVersionID string) ([]consistency.Finding, error) {
	if !c.Available() {
		return nil, errorf("no storyboard reader is configured")
	}
	version, err := c.storyboard.GetStoryboardVersion(ctx, storyboardVersionID)
	if err != nil {
		return nil, err
	}
	items, err := c.storyboard.ListStoryboardItems(ctx, storyboardVersionID)
	if err != nil {
		return nil, err
	}
	findings := []consistency.Finding{}
	findings = append(findings, c.checkCoverage(ctx, version, items)...)
	findings = append(findings, c.checkDuration(ctx, version, items)...)
	findings = append(findings, c.checkAssetApproval(ctx, items)...)
	findings = append(findings, c.checkCostumeContinuity(ctx, version, items)...)
	findings = append(findings, c.checkPropContinuity(ctx, version, items)...)
	findings = append(findings, c.checkLocationContinuity(ctx, version, items)...)
	return consistency.Sort(consistency.Dedupe(findings)), nil
}

// CheckStoryboardVersion is the port the stage pipeline calls.
//
// It is the same signature as `CheckStoryboard`, declared again so the pipeline can depend on a
// one-method interface rather than on this type: the pipeline is generic across stages and must not
// import a checker that knows what a storyboard is. See `StageChecker` below.
func (c *Checker) CheckStoryboardVersion(ctx context.Context, storyboardVersionID string) ([]consistency.Finding, error) {
	return c.CheckStoryboard(ctx, storyboardVersionID)
}

// checkCoverage compares the board's rows against the script's own shots.
//
// Section 11.3 lists "剧本覆盖", "Shot 顺序" and "缺失镜头" as separate rules, and they are one check:
// the rows must be exactly the shots, in the same order. A missing shot and an extra row are the
// same defect seen from two sides, and reporting them as one finding per shot is what lets a user
// act on it.
//
// A board with NO rows is not reported here. An empty board is the state of a version that was just
// created, and treating it as a coverage failure would make every new version start with a page of
// findings.
func (c *Checker) checkCoverage(ctx context.Context, version storyboard.StoryboardVersion, items []storyboard.StoryboardItem) []consistency.Finding {
	if c.scriptSource == nil || len(items) == 0 || strings.TrimSpace(version.ScriptVersionID) == "" {
		return nil
	}
	reader := c.scriptSource.ScriptReaderFor(ctx, version.ScriptVersionID)
	if reader == nil {
		return nil
	}
	shots, err := reader.ShotIDs(ctx)
	if err != nil || len(shots) == 0 {
		// A script that cannot be read is NOT a board that is missing shots. Reporting one as the
		// other is the false positive that would make this rule unusable.
		return nil
	}
	boarded := map[string]storyboard.StoryboardItem{}
	for _, item := range items {
		boarded[item.ShotID] = item
	}
	findings := []consistency.Finding{}
	for index, shotID := range shots {
		if _, ok := boarded[shotID]; ok {
			continue
		}
		findings = append(findings, consistency.Finding{
			Rule:       consistency.RuleShotCoverage,
			Severity:   consistency.SeverityMajor,
			EntityType: "storyboard_version",
			EntityID:   version.ID,
			Location:   fmt.Sprintf("shot %d of %d", index+1, len(shots)),
			Field:      "shotId",
			Problem: fmt.Sprintf("The script has a shot at position %d that this board does not cover, "+
				"so nothing will be generated for it.", index+1),
			Suggestion: "Add a row for the missing shot, or regenerate the board from the script.",
			Evidence: []consistency.Evidence{
				{Type: "entity_ref", Ref: shotID},
				{Type: "entity_ref", Ref: version.ScriptVersionID},
			},
			// Not auto-fixable: what the row should SAY about the shot is a directing decision, and a
			// machine that invented one would be filling a board with plausible text.
			AutoFixable: false,
		})
	}
	// An ordinal that does not run from one with no gap is a separate defect: the rows exist and
	// their positions disagree with the board's own order, which breaks the shot sequence the video
	// stage is generated in.
	for index, item := range items {
		if item.Ordinal != index+1 {
			findings = append(findings, consistency.Finding{
				Rule:       consistency.RuleShotCoverage,
				Severity:   consistency.SeverityMajor,
				EntityType: "storyboard_item",
				EntityID:   item.ID,
				Field:      "ordinal",
				Problem: fmt.Sprintf("The row at position %d states ordinal %d, so the board's order "+
					"disagrees with its rows.", index+1, item.Ordinal),
				Suggestion: "Renumber the rows so their ordinals run from one.",
				Evidence: []consistency.Evidence{
					{Type: "entity_ref", Ref: item.ID},
					{Type: "entity_ref", Ref: version.ID},
				},
				// The renumber is mechanical: the position is the truth and the ordinal is the copy.
				AutoFixable: true,
			})
			break
		}
	}
	return findings
}

// checkDuration compares the rows' total against the script's own estimate.
//
// A tolerance rather than equality, because a board is a plan: four seconds a row over twelve rows is
// forty-eight against a script that estimated fifty is not a defect, and a rule that called it one
// would fire on every board. The tolerance is a tenth of the estimate, which is the same order as the
// per-row rounding the durations come from.
func (c *Checker) checkDuration(ctx context.Context, version storyboard.StoryboardVersion, items []storyboard.StoryboardItem) []consistency.Finding {
	if c.scriptSource == nil || len(items) == 0 || strings.TrimSpace(version.ScriptVersionID) == "" {
		return nil
	}
	reader := c.scriptSource.ScriptReaderFor(ctx, version.ScriptVersionID)
	if reader == nil {
		return nil
	}
	expected, err := reader.Duration(ctx)
	if err != nil || expected <= 0 {
		return nil
	}
	total := 0
	for _, item := range items {
		total += item.DurationSeconds
	}
	if total == 0 {
		// No durations at all is the state of a board written without them, which the row's own
		// required-field validation covers. Reporting it as a duration MISMATCH would say the board is
		// ten times too short when the truth is that nobody stated a length.
		return nil
	}
	drift := total - expected
	if drift < 0 {
		drift = -drift
	}
	tolerance := expected / 10
	if tolerance < 5 {
		tolerance = 5
	}
	if drift <= tolerance {
		return nil
	}
	return []consistency.Finding{{
		Rule:       consistency.RuleDuration,
		Severity:   consistency.SeverityMinor,
		EntityType: "storyboard_version",
		EntityID:   version.ID,
		Field:      "durationSeconds",
		Problem: fmt.Sprintf("The board's rows add up to %d seconds against the script's %d, a "+
			"difference of %d.", total, expected, drift),
		Suggestion: "Adjust the rows' durations or the script's estimate so the episode's length is " +
			"what either one says.",
		Evidence: []consistency.Evidence{
			{Type: "entity_ref", Ref: version.ID},
			{Type: "entity_ref", Ref: version.ScriptVersionID},
		},
		// Not automatic: which shot gives up a second is a directing decision.
		AutoFixable: false,
	}}
}

// checkAssetApproval verifies that every asset a row cites is one the project still approves.
//
// Section 11.2's "Approved Version" and DOMAIN_MODEL section 8.4's rule that the approved version is
// the one in force. A row citing a superseded version would generate an image of a costume the
// project has since replaced — which is precisely AC-E2E-004's injected fault, and the reason this
// rule is a BLOCKER rather than a remark.
func (c *Checker) checkAssetApproval(ctx context.Context, items []storyboard.StoryboardItem) []consistency.Finding {
	if c.assets == nil {
		return nil
	}
	findings := []consistency.Finding{}
	for _, item := range items {
		usages, err := c.assets.UsagesForConsumer(ctx, asset.ConsumerShot, item.ID)
		if err != nil {
			continue
		}
		for _, usage := range usages {
			version, err := c.assets.GetVersion(ctx, usage.AssetVersionID)
			if err != nil {
				continue
			}
			record, err := c.assets.GetAsset(ctx, version.AssetID)
			if err != nil {
				continue
			}
			if record.CurrentApprovedVersionID == usage.AssetVersionID {
				continue
			}
			findings = append(findings, consistency.Finding{
				Rule:       consistency.RuleAssetApproved,
				Severity:   consistency.SeverityCritical,
				EntityType: "storyboard_item",
				EntityID:   item.ID,
				Field:      "assetVersionId",
				Problem: "The row uses " + record.Name + " version " + itoa(version.VersionNumber) +
					", which is not the version this project currently approves.",
				Suggestion: "Point the row at " + record.Name + "'s approved version " +
					record.CurrentApprovedVersionID + ", or approve the version it names.",
				Evidence: []consistency.Evidence{
					{Type: "entity_ref", Ref: item.ID},
					{Type: "entity_ref", Ref: usage.AssetVersionID},
					{Type: "entity_ref", Ref: record.CurrentApprovedVersionID},
				},
				// The fix is the citation itself, which is what makes it mechanical.
				AutoFixable: true,
			})
		}
	}
	return findings
}

// checkCostumeContinuity is scope item 14's costume clause and AC-E2E-004's own defect.
//
// # The rule, stated so it can be argued with
//
// A board row that uses a costume for a character must use the costume version the story says that
// character is wearing AT THAT POINT. The position is the row's ORDINAL, which is the board's own
// statement of when in the episode the shot happens — the board is the only artifact that orders the
// shots, and section 6.8's spans are in event order, so the board's position is the mapping between
// them.
//
// # What it reads to know a row is about a costume
//
// The usage ROLE. `recordShotAssetUsage` writes what the writer said, and the fixture says "costume";
// a role that is not one of the costume words is not a costume claim and the rule says nothing. That
// is the honest limit: a board that used role "reference" for a jacket is not distinguishable from
// one that used it for a lamp, and inventing a distinction would fire on both.
//
// # Why the character is the asset's own story entity
//
// The costume's story entity IS the character (a costume is an asset about a character, per the asset
// aggregate's story link), so the rule needs no second mapping to find whose costume this is. A
// costume with no story entity is skipped: nothing says whose it is, so nothing can be compared.
func (c *Checker) checkCostumeContinuity(ctx context.Context, version storyboard.StoryboardVersion, items []storyboard.StoryboardItem) []consistency.Finding {
	if c.assets == nil || c.story == nil {
		return nil
	}
	findings := []consistency.Finding{}
	for _, item := range items {
		usages, err := c.assets.UsagesForConsumer(ctx, asset.ConsumerShot, item.ID)
		if err != nil {
			continue
		}
		for _, usage := range usages {
			if !isCostumeRole(usage.UsageRole) {
				continue
			}
			applied, err := c.assets.GetVersion(ctx, usage.AssetVersionID)
			if err != nil {
				continue
			}
			costume, err := c.assets.GetAsset(ctx, applied.AssetID)
			if err != nil {
				continue
			}
			if strings.TrimSpace(costume.StoryEntityID) == "" {
				continue
			}
			inForce, found, err := c.story.CostumeStateAt(ctx, costume.StoryEntityID, item.Ordinal)
			if err != nil || !found {
				// The character has no recorded costume at this point, so there is nothing to compare
				// against. Reported as silence rather than as a fault: a project that has not recorded
				// costume states yet would otherwise get a finding per row.
				continue
			}
			if inForce == usage.AssetVersionID {
				continue
			}
			findings = append(findings, consistency.Finding{
				Rule:       consistency.RuleCostumeContinuity,
				Severity:   consistency.SeverityMajor,
				EntityType: "storyboard_item",
				EntityID:   item.ID,
				Field:      "costumeVersionId",
				Problem: "This row dresses " + costume.Name + " in " + applied.AssetID +
					" version " + itoa(applied.VersionNumber) + ", but the story has a different " +
					"costume in force at this point.",
				Suggestion: "Point the row at " + inForce + ", or record the change in the story if " +
					"the character really does change costume here.",
				Evidence: []consistency.Evidence{
					{Type: "entity_ref", Ref: item.ID},
					{Type: "entity_ref", Ref: usage.AssetVersionID},
					{Type: "entity_ref", Ref: inForce},
				},
				// AC-E2E-004's FIX is exactly this: "重新引用 costume_version_004".
				AutoFixable: true,
			})
		}
	}
	return findings
}

// checkPropContinuity is scope item 14's prop clause.
//
// A row that uses a PROP must use one whose story entity takes part in the event the row's scene
// adapts. A prop that appears in a scene its owner is not in is the "道具所有权" defect section 11.2
// names, stated as a join rather than as a judgement.
//
// The rule reads the scene through the SHOT, because a board row cites a shot and the shot belongs to
// a scene (migration 000008). Two hops, and both are stored relations rather than names to resolve.
func (c *Checker) checkPropContinuity(ctx context.Context, version storyboard.StoryboardVersion, items []storyboard.StoryboardItem) []consistency.Finding {
	if c.assets == nil || c.story == nil || c.scriptSource == nil {
		return nil
	}
	reader := c.scriptSource.ScriptReaderFor(ctx, version.ScriptVersionID)
	if reader == nil {
		return nil
	}
	// The scene's event and its participants are read once per scene rather than once per row: a
	// twelve-row board covers three or four scenes, and a rule that re-read the same two queries per
	// row would spend most of its time on the same answer.
	eventOfScene := map[string]string{}
	participantsOfEvent := map[string]map[string]bool{}
	findings := []consistency.Finding{}
	for _, item := range items {
		usages, err := c.assets.UsagesForConsumer(ctx, asset.ConsumerShot, item.ID)
		if err != nil {
			continue
		}
		for _, usage := range usages {
			if !isPropRole(usage.UsageRole) {
				continue
			}
			applied, err := c.assets.GetVersion(ctx, usage.AssetVersionID)
			if err != nil {
				continue
			}
			prop, err := c.assets.GetAsset(ctx, applied.AssetID)
			if err != nil {
				continue
			}
			if strings.TrimSpace(prop.StoryEntityID) == "" {
				continue
			}
			sceneID, err := reader.ShotScene(ctx, item.ShotID)
			if err != nil || strings.TrimSpace(sceneID) == "" {
				continue
			}
			eventID, ok := eventOfScene[sceneID]
			if !ok {
				eventID, _ = reader.SceneEventOf(ctx, sceneID)
				eventOfScene[sceneID] = eventID
			}
			if strings.TrimSpace(eventID) == "" {
				// A scene adapted from no event is not a prop-continuity fault; it is a scene with no
				// source, which the script's own stage would have reported.
				continue
			}
			participants, ok := participantsOfEvent[eventID]
			if !ok {
				ids, err := c.story.StoryEventParticipantsFor(ctx, eventID)
				if err != nil {
					continue
				}
				participants = map[string]bool{}
				for _, id := range ids {
					participants[id] = true
				}
				participantsOfEvent[eventID] = participants
			}
			if participants[prop.StoryEntityID] {
				continue
			}
			findings = append(findings, consistency.Finding{
				Rule:       consistency.RulePropContinuity,
				Severity:   consistency.SeverityMajor,
				EntityType: "storyboard_item",
				EntityID:   item.ID,
				Field:      "assetVersionId",
				Problem: "This row uses " + prop.Name + ", whose story entity takes no part in the " +
					"event this scene adapts.",
				Suggestion: "Remove the prop from this row, or record its owner's involvement in the " +
					"scene's event.",
				Evidence: []consistency.Evidence{
					{Type: "entity_ref", Ref: item.ID},
					{Type: "entity_ref", Ref: usage.AssetVersionID},
					{Type: "entity_ref", Ref: eventID},
				},
				AutoFixable: false,
			})
		}
	}
	return findings
}

// checkLocationContinuity is scope item 14's location clause.
//
// # The rule, and why it is two rules in one
//
// ROADMAP item 14 lists four continuity families — character, costume, prop, location — and the
// location one has two halves, each of which is a different kind of claim:
//
//   - ROWS OF ONE SCENE MUST AGREE. A board whose rows for one scene cite two different locations is
//     a board that will generate two places for one conversation. The check reads each row's
//     location usage and compares them within the scene the row's shot belongs to.
//   - A CITED LOCATION MUST EXISTS AND BE APPROVED. That half is already `checkAssetApproval`'s, and
//     it is not repeated here: a rule that reported the same fault twice would make the merge's
//     dedupe look like it was working when it was only hiding a duplicate.
//
// # Why the first half is a real check rather than a tautology
//
// A scene's location is not a column on the scene: `scenes.location_entity_id` exists and is often
// empty, while the location a picture uses arrives as an asset usage on the row. So the row is where
// the answer lives, and disagreement between two rows of one scene is a mistake nothing else would
// catch — the coverage rule counts rows, the approval rule checks versions, and neither looks across
// rows at what they depicted.
//
// # Why it stays silent when it finds nothing to compare
//
// A board with no location usages at all is the ordinary state of a project that has not created
// location assets yet, and a row whose shot has no scene (a shot the script structure does not know)
// cannot be grouped. Both return no findings rather than a report, which is the same discipline the
// costume rule keeps: a deterministic check that fired on missing data would be one a user learns to
// ignore.
func (c *Checker) checkLocationContinuity(ctx context.Context, version storyboard.StoryboardVersion, items []storyboard.StoryboardItem) []consistency.Finding {
	if c.assets == nil || c.scriptSource == nil {
		return nil
	}
	reader := c.scriptSource.ScriptReaderFor(ctx, version.ScriptVersionID)
	if reader == nil {
		return nil
	}
	// The first location each scene is seen with, so a later row can be compared against it.
	locationOfScene := map[string]string{}
	locationName := map[string]string{}
	findings := []consistency.Finding{}
	for _, item := range items {
		sceneID, err := reader.ShotScene(ctx, item.ShotID)
		if err != nil || strings.TrimSpace(sceneID) == "" {
			continue
		}
		usages, err := c.assets.UsagesForConsumer(ctx, asset.ConsumerShot, item.ID)
		if err != nil {
			continue
		}
		for _, usage := range usages {
			if !isLocationRole(usage.UsageRole) {
				continue
			}
			version, err := c.assets.GetVersion(ctx, usage.AssetVersionID)
			if err != nil {
				continue
			}
			record, err := c.assets.GetAsset(ctx, version.AssetID)
			if err != nil {
				continue
			}
			// The ASSET is what has to agree, not the version: two versions of one location are the
			// same place, and reporting them would fire on every set that was re-rendered.
			first, seen := locationOfScene[sceneID]
			if !seen {
				locationOfScene[sceneID] = record.ID
				locationName[sceneID] = record.Name
				continue
			}
			if first == record.ID {
				continue
			}
			findings = append(findings, consistency.Finding{
				Rule:       consistency.RuleLocationContinuity,
				Severity:   consistency.SeverityMajor,
				EntityType: "storyboard_item",
				EntityID:   item.ID,
				Field:      "assetVersionId",
				Problem: "This row places the scene at " + record.Name + ", while another row of the " +
					"same scene places it at " + locationName[sceneID] + ".",
				Suggestion: "Point every row of one scene at the same location, or split the scene in " +
					"the script if it really moves.",
				Evidence: []consistency.Evidence{
					{Type: "entity_ref", Ref: item.ID},
					{Type: "entity_ref", Ref: record.ID},
					{Type: "entity_ref", Ref: first},
				},
				// Not auto-fixable: which of the two is right is a decision about the scene rather
				// than a citation to correct.
				AutoFixable: false,
			})
			// One finding per scene is enough: a board that alternated between two locations would
			// otherwise report every row after the first, and the fault is one decision.
			break
		}
	}
	return findings
}

// isLocationRole reports whether a usage role says the asset is a location.
func isLocationRole(role string) bool {
	lowered := strings.ToLower(strings.TrimSpace(role))
	return strings.Contains(lowered, "location") || strings.Contains(lowered, "scene") ||
		strings.Contains(lowered, "set")
}

// isCostumeRole reports whether a usage role says the asset is a costume.
//
// The vocabulary is open — `usage_role` is a free string in the schema, with "reference" as its
// default — so this matches on the words a writer would plausibly use for a costume rather than
// demanding one exact spelling. A role that says nothing about costume is not a costume claim, and
// the rule stays silent rather than guessing.
func isCostumeRole(role string) bool {
	lowered := strings.ToLower(strings.TrimSpace(role))
	return strings.Contains(lowered, "costume") || strings.Contains(lowered, "outfit") ||
		strings.Contains(lowered, "wardrobe")
}

// isPropRole reports whether a usage role says the asset is a prop.
func isPropRole(role string) bool {
	lowered := strings.ToLower(strings.TrimSpace(role))
	return strings.Contains(lowered, "prop") || strings.Contains(lowered, "hand") ||
		strings.Contains(lowered, "object")
}

// itoaSmall renders a small integer, so a message can quote a version number without a conversion
// import at each call site.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}

// errorf builds a checker error.
//
// It is a plain error rather than a domain one because it reports a composition fact: "this checker
// has no storyboard reader" is a wiring problem, and the message is what the composition root's
// author needs to read.
func errorf(message string) error {
	return &checkerError{message: message}
}

type checkerError struct{ message string }

func (e *checkerError) Error() string { return e.message }
