package agenttools

import (
	"context"
	"strings"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
)

// production_handlers.go holds the three tools WP-09 added: the shots a storyboard's rows
// are one per, and the gap report's write and read.
//
// They are in their own file rather than appended to handlers.go because they were added
// by a different work package than the rest of the table, and the reason each exists is
// worth stating at its own definition rather than in a list of a hundred others.

// bindReadShots returns a script version's shots, which is what a storyboard rows one per.
//
// THE GAP IT CLOSES is the one the production manifest's own tool list showed: the
// storyboard stage could read `script.read_script_version`, which returns a version row
// with a scene COUNT, and nothing that listed the shots. An agent asked to board twelve
// shots could see that the script had some number of scenes and no shot at all, so the
// rows it wrote could not cite a real shot id — and `storyboard_items.shot_id` has no
// foreign key, which means the board would have accepted them.
//
// The page is over SHOTS rather than scenes, because that is the unit the caller needs:
// a scene's shots are three or four, so paging by scene would still return a whole scene's
// worth and a caller wanting the twelfth shot would have to read everything before it.
func bindReadShots(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			VersionID string `json:"scriptVersionId"`
			SceneID   string `json:"sceneId"`
			Limit     int    `json:"limit"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		versionID, err := required(arguments.VersionID, "version")
		if err != nil {
			return nil, err
		}
		version, err := deps.Script.GetScriptVersion(ctx, versionID)
		if err != nil {
			return nil, err
		}
		script, err := deps.Script.GetScript(ctx, version.ScriptID)
		if err != nil {
			return nil, err
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, script.EpisodeID); err != nil {
			return nil, err
		}
		structure, err := deps.Script.GetScriptStructure(ctx, version.ID)
		if err != nil {
			return nil, err
		}
		sceneFilter := strings.TrimSpace(arguments.SceneID)
		views := make([]map[string]any, 0, 16)
		total := 0
		for _, scene := range structure.Scenes {
			if sceneFilter != "" && scene.ID != sceneFilter {
				continue
			}
			for _, shot := range scene.Shots {
				total++
				views = append(views, map[string]any{
					"shotId": shot.ID,
					// The scene travels with every shot because a storyboard row must
					// name the scene it belongs to (FR-070's "Scene"), and a caller that
					// had to read the scenes separately would be making a second call to
					// re-derive a fact this one already has.
					"sceneId":     scene.ID,
					"sceneNumber": scene.SceneNumber,
					"slugline":    scene.Slugline,
					// FR-070's "上游 StoryEvent" is the SCENE's, not the shot's: a shot
					// has no source event column (§7.5 gives one to the scene and the
					// dialogue line), so a storyboard row cites the event through its
					// scene. Reporting a shot-level field that does not exist would be a
					// value this tool invented.
					"sourceStoryEventId": scene.SourceStoryEventID,
					"ordinal":            shot.Ordinal,
					"shotNumber":         shot.ShotNumber,
					"shotSize":           shot.ShotSize,
					"cameraAngle":        shot.CameraAngle,
					"cameraMovement":     shot.CameraMovement,
					"visualDescription":  shot.VisualDescription,
					"actionDescription":  shot.ActionDescription,
					"estimatedDuration":  shot.EstimatedDurationSeconds,
					"status":             string(shot.Status),
				})
			}
		}
		limit := limitOf(arguments.Limit, 200, 400)
		returned := views
		if len(returned) > limit {
			returned = returned[:limit]
		}
		return map[string]any{
			"versionId": version.ID,
			"episodeId": script.EpisodeID,
			"sceneId":   sceneFilter,
			"shots":     returned,
			"returned":  len(returned),
			"total":     total,
		}, nil
	}
}

// bindCreateGapReport records an asset gap analysis.
//
// IT WRITES A DRAFT AND CANNOT APPROVE, and both halves are section 10.1's: asset_analysis
// has a required user gate and a supervisor of NONE, so the model's job is to say what it
// found and a person's is to put it in force. A tool that could approve would make the
// gate a formality.
//
// The ordinals come from the ARRAY'S ORDER rather than from the caller, which is §17's
// rule that order and uniqueness are the code's job: a model that supplied its own
// ordinals could leave a hole or repeat one, and the schema would then refuse a write the
// model had no way to fix.
func bindCreateGapReport(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			EpisodeID        string `json:"episodeId"`
			ScriptVersionID  string `json:"scriptVersionId"`
			Summary          string `json:"summary"`
			BasedOnVersionID string `json:"basedOnVersionId"`
			Items            []struct {
				AssetType       string `json:"assetType"`
				StoryEntityID   string `json:"storyEntityId"`
				StoryEntityName string `json:"storyEntityName"`
				AssetID         string `json:"assetId"`
				Status          string `json:"status"`
				UsageRole       string `json:"usageRole"`
				Required        bool   `json:"required"`
				Notes           string `json:"notes"`
			} `json:"items"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		episodeID, err := required(arguments.EpisodeID, "episode")
		if err != nil {
			return nil, err
		}
		scriptVersionID, err := required(arguments.ScriptVersionID, "script version")
		if err != nil {
			return nil, err
		}
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, episodeID); err != nil {
			return nil, err
		}
		items := make([]appassets.GapItemInput, 0, len(arguments.Items))
		for _, item := range arguments.Items {
			items = append(items, appassets.GapItemInput{
				AssetType:       asset.Type(strings.TrimSpace(item.AssetType)),
				StoryEntityID:   item.StoryEntityID,
				StoryEntityName: item.StoryEntityName,
				AssetID:         item.AssetID,
				Status:          asset.GapStatus(strings.TrimSpace(item.Status)),
				UsageRole:       item.UsageRole,
				Required:        item.Required,
				Notes:           item.Notes,
			})
		}
		createdBy, createdByID := agentActor(request)
		report, written, err := deps.Gaps.CreateGapReport(ctx, appassets.CreateGapReportRequest{
			EpisodeID:        episodeID,
			ScriptVersionID:  scriptVersionID,
			Summary:          arguments.Summary,
			BasedOnVersionID: arguments.BasedOnVersionID,
			SourceAgentRunID: createdByID,
			Items:            items,
			CreatedByType:    createdBy,
			CreatedByID:      createdByID,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"reportId":      report.ID,
			"versionNumber": report.VersionNumber,
			"status":        string(report.Status),
			"itemCount":     len(written),
			"missing":       countMissing(written),
		}, nil
	}
}

// countMissing reports how many lines have no asset, so a caller can tell an analysis
// that found nothing from one that found nothing missing without reading every line.
func countMissing(items []asset.GapItem) int {
	missing := 0
	for _, item := range items {
		if item.Status == asset.GapMissing {
			missing++
		}
	}
	return missing
}

// bindReadGapReport returns one report and its lines.
//
// It answers by REPORT ID or, when none is given, by the episode's APPROVED report — the
// two questions a caller actually has. The second is what a batch's gate reads, and
// `UnresolvedRequiredItems` is the service's own answer to "may this batch run", so this
// tool states the same facts rather than a second opinion about them.
func bindReadGapReport(deps Deps) agentruntime.ToolHandler {
	return func(ctx context.Context, request agentruntime.ToolRequest) (any, error) {
		if err := contextDone(ctx); err != nil {
			return nil, err
		}
		var arguments struct {
			ReportID  string `json:"reportId"`
			EpisodeID string `json:"episodeId"`
		}
		if err := decodeArguments(request.Arguments, &arguments); err != nil {
			return nil, err
		}
		reportID := strings.TrimSpace(arguments.ReportID)
		episodeID := strings.TrimSpace(arguments.EpisodeID)
		if reportID == "" && episodeID == "" {
			return nil, agent.InvalidError("A gap report read must name a report or an episode.")
		}
		var report asset.GapReport
		var items []asset.GapItem
		if reportID != "" {
			found, foundItems, err := deps.Gaps.GetGapReport(ctx, reportID)
			if err != nil {
				return nil, err
			}
			report, items = found, foundItems
		} else {
			if err := assertEpisodeInProject(ctx, deps, request.ProjectID, episodeID); err != nil {
				return nil, err
			}
			approved, ok, err := deps.Gaps.CurrentApprovedGapReport(ctx, episodeID)
			if err != nil {
				return nil, err
			}
			if !ok {
				// NO APPROVED REPORT is stated as such rather than as an empty list: a
				// caller that read "no unresolved items" from this would conclude the
				// episode is ready, and the opposite is true — nothing has analysed it.
				return map[string]any{
					"episodeId":        episodeID,
					"approvedReportId": "",
					"note":             "This episode has no approved gap report.",
				}, nil
			}
			// The lines come from the SAME call that read the report, so a report whose
			// items were written between the two reads cannot be reported with another
			// report's lines.
			found, foundItems, err := deps.Gaps.GetGapReport(ctx, approved.ID)
			if err != nil {
				return nil, err
			}
			report, items = found, foundItems
		}
		// The scope check runs against the report's OWN episode, which is what makes a
		// report id from another project a refusal rather than a read.
		if err := assertEpisodeInProject(ctx, deps, request.ProjectID, report.EpisodeID); err != nil {
			return nil, err
		}
		lines := make([]map[string]any, 0, len(items))
		for _, item := range items {
			lines = append(lines, map[string]any{
				"ordinal":         item.Ordinal,
				"assetType":       string(item.AssetType),
				"storyEntityId":   item.StoryEntityID,
				"storyEntityName": item.StoryEntityName,
				"assetId":         item.AssetID,
				"status":          string(item.Status),
				"required":        item.Required,
				"usageRole":       item.UsageRole,
				"notes":           item.Notes,
			})
		}
		return map[string]any{
			"reportId":           report.ID,
			"episodeId":          report.EpisodeID,
			"versionNumber":      report.VersionNumber,
			"status":             string(report.Status),
			"summary":            report.Summary,
			"items":              lines,
			"unresolvedRequired": unresolvedRequiredViews(items),
		}, nil
	}
}

// unresolvedRequiredViews renders the lines that block a batch.
//
// It is the SAME predicate the gate uses — `asset.UnresolvedRequiredItems` — rather than a
// filter written again here, so a caller reading this tool's answer and a caller asking the
// gate get the same list.
func unresolvedRequiredViews(items []asset.GapItem) []map[string]any {
	blocking := asset.UnresolvedRequiredItems(items)
	views := make([]map[string]any, 0, len(blocking))
	for _, item := range blocking {
		views = append(views, map[string]any{
			"ordinal":         item.Ordinal,
			"assetType":       string(item.AssetType),
			"storyEntityId":   item.StoryEntityID,
			"storyEntityName": item.StoryEntityName,
			"notes":           item.Notes,
		})
	}
	return views
}
