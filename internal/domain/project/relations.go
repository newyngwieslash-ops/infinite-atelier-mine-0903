package project

// The relation registry of DOMAIN_MODEL §10.4.
//
// WP-04 declared only the relation *names*, and its own comment recorded why:
// "validating the semantic relations themselves is WP-05's job". This file is
// that job. Each registered relation carries the seven fields §10.4 defines:
//
//	relation_type
//	allowed_source_types
//	allowed_target_types
//	directionality
//	cardinality
//	creates_domain_relation
//	requires_version
//	validation_handler
//
// The eighth, validation_handler, is the name of the check to run. It is a
// label rather than a function because the registry is data the schema and the
// UI both read; ValidateEdgeRelation below is what executes the check.

// EntityRefType is what a canvas node can project, and what a registered
// relation may therefore connect.
//
// The list covers the drama entities of DOMAIN_MODEL §4-§11 plus the free
// canvas node kinds that carry no entity. It is the vocabulary
// canvas_nodes.entity_type is written in, and it is what the relation registry
// validates against.
type EntityRefType string

const (
	EntityProjectSettings   EntityRefType = "project_settings"
	EntityProjectRule       EntityRefType = "project_rule"
	EntityStyleGuide        EntityRefType = "style_guide"
	EntitySourceDocument    EntityRefType = "source_document"
	EntityChapter           EntityRefType = "chapter"
	EntityStoryEntity       EntityRefType = "story_entity"
	EntityStoryEvent        EntityRefType = "story_event"
	EntityStoryRelation     EntityRefType = "story_relation"
	EntityCharacterState    EntityRefType = "character_state"
	EntityEpisode           EntityRefType = "episode"
	EntityStorySkeleton     EntityRefType = "story_skeleton"
	EntityAdaptationStrat   EntityRefType = "adaptation_strategy"
	EntityScript            EntityRefType = "script"
	EntityScriptVersion     EntityRefType = "script_version"
	EntityScene             EntityRefType = "scene"
	EntityDialogueLine      EntityRefType = "dialogue_line"
	EntityShot              EntityRefType = "shot"
	EntityAsset             EntityRefType = "asset"
	EntityAssetVersion      EntityRefType = "asset_version"
	EntityDirectorPlan      EntityRefType = "director_plan"
	EntityStoryboard        EntityRefType = "storyboard"
	EntityStoryboardVersion EntityRefType = "storyboard_version"
	EntityStoryboardItem    EntityRefType = "storyboard_item"
	EntityStoryboardPanel   EntityRefType = "storyboard_panel"
	EntityWorkflowRun       EntityRefType = "workflow_run"
	EntityStageRun          EntityRefType = "stage_run"
	EntityReviewReport      EntityRefType = "review_report"
	EntityGenerationJob     EntityRefType = "generation_job"
	// EntityFreeNode is a node that projects nothing, such as a text or image
	// node a user placed on a free canvas.
	EntityFreeNode EntityRefType = "free_node"
)

// EntityRefTypes lists the documented reference kinds in the schema's order.
var EntityRefTypes = []EntityRefType{
	EntityProjectSettings, EntityProjectRule, EntityStyleGuide,
	EntitySourceDocument, EntityChapter,
	EntityStoryEntity, EntityStoryEvent, EntityStoryRelation, EntityCharacterState,
	EntityEpisode, EntityStorySkeleton, EntityAdaptationStrat,
	EntityScript, EntityScriptVersion, EntityScene, EntityDialogueLine, EntityShot,
	EntityAsset, EntityAssetVersion,
	EntityDirectorPlan, EntityStoryboard, EntityStoryboardVersion, EntityStoryboardItem, EntityStoryboardPanel,
	EntityWorkflowRun, EntityStageRun, EntityReviewReport, EntityGenerationJob,
	EntityFreeNode,
}

// IsValidEntityRefType reports whether a node may declare that reference kind.
func IsValidEntityRefType(value EntityRefType) bool {
	for _, candidate := range EntityRefTypes {
		if candidate == value {
			return true
		}
	}
	return false
}

// Directionality says whether a relation reads one way or both.
type Directionality string

const (
	// Directional means source and target are not interchangeable.
	Directional Directionality = "directional"
	// Bidirectional means the relation holds either way.
	Bidirectional Directionality = "bidirectional"
)

// Cardinality says how many of the relation one node may have.
type Cardinality string

const (
	// CardinalityOneToOne allows at most one such relation per node.
	CardinalityOneToOne Cardinality = "one_to_one"
	// CardinalityOneToMany allows many, which is the common case (a scene
	// referencing several assets, a shot using several characters).
	CardinalityOneToMany Cardinality = "one_to_many"
)

// ValidationHandler names the check a relation runs when its endpoints change.
type ValidationHandler string

const (
	// HandlerEndpointTypes checks only that source and target kinds are allowed.
	HandlerEndpointTypes ValidationHandler = "endpoint_types"
	// HandlerVersionOwnership additionally requires entity_version_id to belong
	// to the entity the node projects.
	HandlerVersionOwnership ValidationHandler = "version_ownership"
	// HandlerSameProject requires both endpoints to belong to one project, so a
	// relation cannot silently link entities of two different dramas.
	HandlerSameProject ValidationHandler = "same_project"
)

// RelationDefinition is one row of the §10.4 registry.
type RelationDefinition struct {
	Type              RelationType
	AllowedSource     []EntityRefType
	AllowedTarget     []EntityRefType
	Directionality    Directionality
	Cardinality       Cardinality
	CreatesDomainRel  bool
	RequiresVersion   bool
	ValidationHandler ValidationHandler
}

// relationRegistry is the §10.4 registry plus the two PRD FR-130 additions and
// the generic fallback.
//
// PRD FR-130 lists uses_character, uses_location and uses_prop, which the domain
// model's list does not; §10.4 lists uses_asset, appears_in and located_in,
// which FR-130 does not. Both are needed — FR-130's three are what a shot uses
// to name its cast, and the domain model's appears_in and located_in describe a
// character's presence in a scene — so the registry is the union. ADR-0007
// records the ruling.
var relationRegistry = []RelationDefinition{
	{
		// generic is the type a legacy untyped connection becomes (PRD FR-130:
		// "旧无类型连线可迁移为 generic，不丢失"). It is deliberately unconstrained:
		// the whole point is that an imported link had no declared meaning, so
		// validating it against a type table would reject the data the import
		// exists to preserve.
		Type:              RelationGeneric,
		Directionality:    Directional,
		Cardinality:       CardinalityOneToMany,
		CreatesDomainRel:  false,
		RequiresVersion:   false,
		ValidationHandler: HandlerEndpointTypes,
	},
	{
		Type:              "contains",
		AllowedSource:     []EntityRefType{EntityScript, EntityScriptVersion, EntityScene, EntityEpisode, EntityStoryboard},
		AllowedTarget:     []EntityRefType{EntityScene, EntityShot, EntityDialogueLine, EntityStoryboardItem},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToMany,
		CreatesDomainRel:  false,
		RequiresVersion:   false,
		ValidationHandler: HandlerSameProject,
	},
	{
		Type:              "adapts_to",
		AllowedSource:     []EntityRefType{EntityScriptVersion, EntityStorySkeleton, EntityAdaptationStrat},
		AllowedTarget:     []EntityRefType{EntitySourceDocument, EntityChapter, EntityStoryEvent},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToMany,
		CreatesDomainRel:  true,
		RequiresVersion:   true,
		ValidationHandler: HandlerVersionOwnership,
	},
	{
		// references means "content points at content". PRD FR-130's own example
		// puts a character on the from side and a shot on the to
		// (fromNodeId: character_1, toNodeId: shot_12), while the domain model's
		// prose leans the other way ("Shot 使用的角色"), so both directions are
		// allowed between content artifacts. What is excluded is pointing at
		// orchestration or configuration records: a shot does not "reference" a
		// workflow run, it was generated by one, which is why generated_by and
		// reviewed_by exist as separate relations.
		Type: "references",
		AllowedSource: []EntityRefType{
			EntityShot, EntityScene, EntityStoryboardItem, EntityStoryboardPanel,
			EntityScriptVersion, EntityStoryEntity, EntityCharacterState,
			EntityAsset, EntityAssetVersion,
		},
		AllowedTarget: []EntityRefType{
			EntityShot, EntityScene, EntityStoryboardItem, EntityStoryboardPanel,
			EntityScriptVersion, EntityStoryEntity, EntityCharacterState,
			EntityAsset, EntityAssetVersion,
		},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToMany,
		CreatesDomainRel:  false,
		RequiresVersion:   false,
		ValidationHandler: HandlerSameProject,
	},
	{
		Type:              "derived_from",
		AllowedSource:     []EntityRefType{EntityAssetVersion, EntityScriptVersion, EntityStoryboardPanel},
		AllowedTarget:     []EntityRefType{EntityAssetVersion, EntityScriptVersion, EntityStoryboardPanel, EntityGenerationJob},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToMany,
		CreatesDomainRel:  true,
		RequiresVersion:   true,
		ValidationHandler: HandlerVersionOwnership,
	},
	{
		Type:              "continues_from",
		AllowedSource:     []EntityRefType{EntityEpisode, EntityScriptVersion, EntityScene},
		AllowedTarget:     []EntityRefType{EntityEpisode, EntityScriptVersion, EntityScene},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToOne,
		CreatesDomainRel:  false,
		RequiresVersion:   false,
		ValidationHandler: HandlerSameProject,
	},
	{
		Type:              "generated_by",
		AllowedSource:     []EntityRefType{EntityAssetVersion, EntityStoryboardPanel, EntityScriptVersion, EntityShot},
		AllowedTarget:     []EntityRefType{EntityGenerationJob, EntityStageRun},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToOne,
		CreatesDomainRel:  false,
		RequiresVersion:   false,
		ValidationHandler: HandlerEndpointTypes,
	},
	{
		Type:              "reviewed_by",
		AllowedSource:     []EntityRefType{EntityStoryboardPanel, EntityScriptVersion, EntityShot, EntityAssetVersion},
		AllowedTarget:     []EntityRefType{EntityReviewReport, EntityStageRun},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToMany,
		CreatesDomainRel:  false,
		RequiresVersion:   false,
		ValidationHandler: HandlerEndpointTypes,
	},
	{
		Type:              "supersedes",
		AllowedSource:     []EntityRefType{EntityAssetVersion, EntityScriptVersion, EntityStoryboardPanel, EntityStoryboardVersion, EntityStyleGuide},
		AllowedTarget:     []EntityRefType{EntityAssetVersion, EntityScriptVersion, EntityStoryboardPanel, EntityStoryboardVersion, EntityStyleGuide},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToOne,
		CreatesDomainRel:  true,
		RequiresVersion:   true,
		ValidationHandler: HandlerVersionOwnership,
	},
	{
		Type:              "first_frame_of",
		AllowedSource:     []EntityRefType{EntityAssetVersion},
		AllowedTarget:     []EntityRefType{EntityShot, EntityStoryboardItem, EntityStoryboardPanel},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToOne,
		CreatesDomainRel:  false,
		RequiresVersion:   true,
		ValidationHandler: HandlerVersionOwnership,
	},
	{
		Type:              "last_frame_of",
		AllowedSource:     []EntityRefType{EntityAssetVersion},
		AllowedTarget:     []EntityRefType{EntityShot, EntityStoryboardItem, EntityStoryboardPanel},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToOne,
		CreatesDomainRel:  false,
		RequiresVersion:   true,
		ValidationHandler: HandlerVersionOwnership,
	},
	{
		// FR-130's uses_character. The source is what uses the asset, the target
		// is the asset version; that reading matches the sentence "Agent 能通过
		// 语义关系查询 Shot 使用的角色、地点和参考资产".
		Type:              "uses_character",
		AllowedSource:     []EntityRefType{EntityShot, EntityScene, EntityStoryboardItem, EntityStoryboardPanel},
		AllowedTarget:     []EntityRefType{EntityAssetVersion, EntityAsset},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToMany,
		CreatesDomainRel:  false,
		RequiresVersion:   true,
		ValidationHandler: HandlerVersionOwnership,
	},
	{
		Type:              "uses_location",
		AllowedSource:     []EntityRefType{EntityShot, EntityScene, EntityStoryboardItem, EntityStoryboardPanel},
		AllowedTarget:     []EntityRefType{EntityAssetVersion, EntityAsset},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToMany,
		CreatesDomainRel:  false,
		RequiresVersion:   true,
		ValidationHandler: HandlerVersionOwnership,
	},
	{
		Type:              "uses_prop",
		AllowedSource:     []EntityRefType{EntityShot, EntityScene, EntityStoryboardItem, EntityStoryboardPanel},
		AllowedTarget:     []EntityRefType{EntityAssetVersion, EntityAsset},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToMany,
		CreatesDomainRel:  false,
		RequiresVersion:   true,
		ValidationHandler: HandlerVersionOwnership,
	},
	{
		Type:              "uses_asset",
		AllowedSource:     []EntityRefType{EntityShot, EntityScene, EntityStoryboardItem, EntityStoryboardPanel, EntityProjectSettings},
		AllowedTarget:     []EntityRefType{EntityAssetVersion, EntityAsset},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToMany,
		CreatesDomainRel:  false,
		RequiresVersion:   true,
		ValidationHandler: HandlerVersionOwnership,
	},
	{
		Type:              "appears_in",
		AllowedSource:     []EntityRefType{EntityStoryEntity, EntityCharacterState},
		AllowedTarget:     []EntityRefType{EntityScene, EntityEpisode, EntityShot},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToMany,
		CreatesDomainRel:  true,
		RequiresVersion:   false,
		ValidationHandler: HandlerSameProject,
	},
	{
		Type:              "located_in",
		AllowedSource:     []EntityRefType{EntityStoryEntity, EntityStoryEvent, EntityCharacterState, EntityScene},
		AllowedTarget:     []EntityRefType{EntityStoryEntity, EntityAsset},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToMany,
		CreatesDomainRel:  true,
		RequiresVersion:   false,
		ValidationHandler: HandlerSameProject,
	},
	{
		Type:              "causes",
		AllowedSource:     []EntityRefType{EntityStoryEvent, EntityStoryRelation},
		AllowedTarget:     []EntityRefType{EntityStoryEvent, EntityStoryRelation},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToMany,
		CreatesDomainRel:  true,
		RequiresVersion:   false,
		ValidationHandler: HandlerSameProject,
	},
	{
		Type:              "precedes",
		AllowedSource:     []EntityRefType{EntityStoryEvent, EntityScene, EntityShot},
		AllowedTarget:     []EntityRefType{EntityStoryEvent, EntityScene, EntityShot},
		Directionality:    Directional,
		Cardinality:       CardinalityOneToMany,
		CreatesDomainRel:  true,
		RequiresVersion:   false,
		ValidationHandler: HandlerSameProject,
	},
	{
		Type:              "contradicts",
		AllowedSource:     []EntityRefType{EntityStoryEvent, EntityStoryEntity, EntityStoryRelation, EntityCharacterState},
		AllowedTarget:     []EntityRefType{EntityStoryEvent, EntityStoryEntity, EntityStoryRelation, EntityCharacterState},
		Directionality:    Bidirectional,
		Cardinality:       CardinalityOneToMany,
		CreatesDomainRel:  true,
		RequiresVersion:   false,
		ValidationHandler: HandlerSameProject,
	},
}

// relationIndex is the registry keyed for lookup.
var relationIndex = func() map[RelationType]RelationDefinition {
	index := make(map[RelationType]RelationDefinition, len(relationRegistry))
	for _, definition := range relationRegistry {
		index[definition.Type] = definition
	}
	return index
}()

// Relation returns the registry entry for a relation type.
func Relation(value RelationType) (RelationDefinition, bool) {
	definition, ok := relationIndex[value]
	return definition, ok
}

// RelationDefinitions returns the registry in declaration order.
func RelationDefinitions() []RelationDefinition {
	definitions := make([]RelationDefinition, len(relationRegistry))
	copy(definitions, relationRegistry)
	return definitions
}

// RelationAllowsSource reports whether a source kind may carry the relation.
func (d RelationDefinition) RelationAllowsSource(kind EntityRefType) bool {
	// A relation with no declared source list accepts any registered kind. Only
	// the generic fallback is in that state, and it is deliberately open.
	if len(d.AllowedSource) == 0 {
		return IsValidEntityRefType(kind)
	}
	for _, candidate := range d.AllowedSource {
		if candidate == kind {
			return true
		}
	}
	return false
}

// AllowsTarget reports whether a target kind may receive the relation.
func (d RelationDefinition) AllowsTarget(kind EntityRefType) bool {
	if len(d.AllowedTarget) == 0 {
		return IsValidEntityRefType(kind)
	}
	for _, candidate := range d.AllowedTarget {
		if candidate == kind {
			return true
		}
	}
	return false
}
