package agentassembly

import (
	"testing"
)

// model_policy_test.go is RP-06.5's table-driven resolution contract: the
// fixed order (call > stage > layer > first-enabled > refuse), provenance
// recorded per level, and an empty resolution REFUSED rather than silently
// swapped.

func TestRP06ModelPolicyResolutionOrder(t *testing.T) {
	cases := []struct {
		name           string
		stageProvider  string
		stageModel     string
		layerProvider  string
		firstEnabled   string
		wantProvider   string
		wantModel      string
		wantProvenance string
		wantErr        bool
	}{
		{
			name:          "stage override wins over layer and default",
			stageProvider: "prov-stage", stageModel: "m-stage",
			layerProvider: "prov-layer", firstEnabled: "prov-first",
			wantProvider: "prov-stage", wantModel: "m-stage", wantProvenance: "stage",
		},
		{
			name:          "layer policy when no stage override",
			layerProvider: "prov-layer", firstEnabled: "prov-first",
			wantProvider: "prov-layer", wantProvenance: "layer",
		},
		{
			name:         "first enabled when nothing else names one",
			firstEnabled: "prov-first",
			wantProvider: "prov-first", wantProvenance: "default",
		},
		{
			name:    "empty resolution is a refusal, not a silent swap",
			wantErr: true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			resolved, err := ResolveStageModelPolicy(
				testCase.stageProvider, testCase.stageModel,
				testCase.layerProvider, testCase.firstEnabled)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("%s resolved to %+v", testCase.name, resolved)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s refused: %v", testCase.name, err)
			}
			if resolved.ProviderID != testCase.wantProvider || resolved.ProviderSource != testCase.wantProvenance {
				t.Fatalf("%s = %+v, want provider %s from %s", testCase.name, resolved, testCase.wantProvider, testCase.wantProvenance)
			}
			if resolved.ModelID != testCase.wantModel {
				t.Fatalf("%s model = %q, want %q", testCase.name, resolved.ModelID, testCase.wantModel)
			}
		})
	}
}

// TestRP06StageModelKeysCoverTheSpec pins the key vocabulary against FR-140's
// list: seven keys, each naming its stage. A key removed here is a silent
// capability removal.
func TestRP06StageModelKeysCoverTheSpec(t *testing.T) {
	for _, key := range []string{
		"script_decision_model", "script_execution_model", "script_supervision_model",
		"image_generation_model", "video_generation_model", "tts_model", "embedding_model",
	} {
		if _, ok := StageModelKeys[key]; !ok {
			t.Fatalf("FR-140's key %q is missing from the stage map", key)
		}
	}
	if len(StageModelKeys) != 7 {
		t.Fatalf("StageModelKeys holds %d entries, want exactly the spec's seven", len(StageModelKeys))
	}
}
