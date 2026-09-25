package media

import (
	"strings"
	"testing"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
)

// mix_test.go grades the filtergraph itself, before ffmpeg is asked to run it.
//
// # Why the graph is graded separately from the sound
//
// The graph is where mixing is either right or wrong, and two of its properties are DEFECTS if they
// are missing rather than nuances — the missing `normalize=0` makes the mix quieter as the episode
// gets busier, and a stray `-shortest` truncates the film to its sound. Both produce a file that
// plays, so a test that only asserts "audio is present" would pass either way. Asserting the
// argument text is what makes them checkable, and it costs nothing: no subprocess, no fixture.
//
// The engine's own test asserts that audio ARRIVES in a real file (see `compose_test.go`), so the two
// together cover the graph and the sound.

func TestTheMixGraphDelaysEveryClipToItsStart(t *testing.T) {
	mix := appmedia.AudioMix{Clips: []appmedia.AudioClip{
		{Role: appmedia.AudioRoleDialogue, Path: "/tmp/line.m4a", StartMS: 4000, Label: "shot 2 line 1"},
		{Role: appmedia.AudioRoleDialogue, Path: "/tmp/line2.m4a", StartMS: 8000, Label: "shot 3 line 1"},
	}}.Normalized()

	graph, err := audioMixGraph(mix)
	if err != nil {
		t.Fatalf("audioMixGraph: %v", err)
	}
	// Each clip is delayed to its own start, and the SAME value is given to both channels: the
	// single-value form applies to the first channel only, which would move a stereo clip's right
	// channel to the start and leave the two out of phase.
	if !strings.Contains(graph, "[1:a:0]adelay=4000|4000") {
		t.Fatalf("the first clip is not delayed to 4s: %s", graph)
	}
	if !strings.Contains(graph, "[2:a:0]adelay=8000|8000") {
		t.Fatalf("the second clip is not delayed to 8s: %s", graph)
	}
	// The input index is one-based and counts the joined picture as input 0, which is what ties the
	// graph to the `-i` arguments the caller built.
	if strings.Contains(graph, "[0:a:0]") {
		t.Fatalf("the graph refers to input 0, which is the picture: %s", graph)
	}
}

func TestTheMixGraphDisablesAmixNormalisation(t *testing.T) {
	// amix's default NORMALISES, dividing every input by the number of inputs — so three dialogue
	// lines would each play at a third of their level and the mix would get quieter as the episode got
	// busier. This is the assertion that a later reader cannot remove the flag without failing.
	mix := appmedia.AudioMix{Clips: []appmedia.AudioClip{
		{Role: appmedia.AudioRoleDialogue, Path: "/tmp/a.m4a"},
		{Role: appmedia.AudioRoleDialogue, Path: "/tmp/b.m4a"},
		{Role: appmedia.AudioRoleMusic, Path: "/tmp/bed.m4a"},
	}}.Normalized()

	graph, err := audioMixGraph(mix)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(graph, "normalize=0") {
		t.Fatalf("the mix normalises, so its level falls as clips are added: %s", graph)
	}
	if !strings.Contains(graph, "amix=inputs=3") {
		t.Fatalf("the mix does not name three inputs: %s", graph)
	}
	if !strings.Contains(graph, "duration=longest") {
		t.Fatalf("the mix would be cut to its shortest clip: %s", graph)
	}
	if !strings.Contains(graph, "[mixed]") {
		t.Fatalf("the mix's output is not labelled as the argument builder expects: %s", graph)
	}
}

func TestTheMixGraphSetsEachClipGain(t *testing.T) {
	// The music default is the one mixing decision this build makes for a user, so it has to reach the
	// arguments: a bed that quietly played at unity is the defect the default exists to prevent.
	mix := appmedia.AudioMix{Clips: []appmedia.AudioClip{
		{Role: appmedia.AudioRoleDialogue, Path: "/tmp/line.m4a"},
		{Role: appmedia.AudioRoleMusic, Path: "/tmp/bed.m4a"},
	}}.Normalized()

	graph, err := audioMixGraph(mix)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(graph, "volume=1") {
		t.Fatalf("the dialogue's unity gain is missing: %s", graph)
	}
	if !strings.Contains(graph, "volume=0.35") {
		t.Fatalf("the music's default gain is missing: %s", graph)
	}
}

func TestTheMixGraphTrimsAClipThatStatesADuration(t *testing.T) {
	// A stated duration is a TRIM, which is a different act from placement: a user who wants the last
	// two seconds off a bed changes the duration and not the start.
	mix := appmedia.AudioMix{Clips: []appmedia.AudioClip{
		{Role: appmedia.AudioRoleMusic, Path: "/tmp/bed.m4a", StartMS: 250, DurationMS: 1500},
	}}.Normalized()

	graph, err := audioMixGraph(mix)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(graph, "atrim=0:1.500") {
		t.Fatalf("the clip's duration did not become a trim: %s", graph)
	}
	if !strings.Contains(graph, "adelay=250|250") {
		t.Fatalf("the trimmed clip is not placed at its start: %s", graph)
	}
	// The trim comes BEFORE the delay, so the delay is measured from the trimmed clip's own
	// beginning — the other order would delay a clip and then trim its head off.
	if strings.Index(graph, "atrim") > strings.Index(graph, "adelay") {
		t.Fatalf("the trim runs after the delay, so the clip's head would be cut instead: %s", graph)
	}
}

func TestTheMixGraphCarriesNoPathAndNoUserText(t *testing.T) {
	// The filtergraph is the one place in this adapter where an expression is built from strings, and
	// the reason it is safe is that nothing but indices, numbers and constants reaches it: the paths
	// are `-i` arguments, checked by `checkPathArgument`, and the labels are never in the graph.
	mix := appmedia.AudioMix{Clips: []appmedia.AudioClip{
		{Role: appmedia.AudioRoleEffect, Path: "/tmp/with:colon/and'quote.m4a", StartMS: 100},
	}}.Normalized()

	graph, err := audioMixGraph(mix)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"/tmp", "colon", "quote", "m4a"} {
		if strings.Contains(graph, forbidden) {
			t.Fatalf("the graph carries %q, and only indices and numbers belong in it: %s", forbidden, graph)
		}
	}
}

func TestTheMixRefusesMoreClipsThanOneCompositionMixes(t *testing.T) {
	// The bound is checked by the application layer too, and it is checked here because this function
	// builds an ARGUMENT: a clip list past the bound would produce a command line no operating system
	// would accept, and the refusal should name the count rather than arrive as an exec failure.
	clips := make([]appmedia.AudioClip, 0, appmedia.MaxAudioClips()+1)
	for index := 0; index <= appmedia.MaxAudioClips(); index++ {
		clips = append(clips, appmedia.AudioClip{Role: appmedia.AudioRoleDialogue, Path: "/tmp/line.m4a"})
	}
	if _, err := audioMixGraph(appmedia.AudioMix{Clips: clips}); err == nil {
		t.Fatalf("a mix of %d clips was accepted, and the bound is %d", len(clips), appmedia.MaxAudioClips())
	}
	// An empty mix is refused HERE even though it is legal for the film, because this function's
	// contract is "build a graph for some clips": the caller decides whether there is sound at all,
	// which is what keeps "a silent film" and "a mix of nothing" from being the same request.
	if _, err := audioMixGraph(appmedia.AudioMix{}); err == nil {
		t.Fatal("an empty mix produced a graph")
	}
}

func TestTheMixGraphOrdersItsInputsByRole(t *testing.T) {
	// The order does not change the sound — the mix sums — but it decides ffmpeg's input NUMBERING,
	// which is what a failure a reader sees refers to. Dialogue first means "the third input is a
	// line" stays true as a project grows.
	mix := appmedia.AudioMix{Clips: []appmedia.AudioClip{
		{Role: appmedia.AudioRoleMusic, Path: "/tmp/bed.m4a"},
		{Role: appmedia.AudioRoleDialogue, Path: "/tmp/line-a.m4a"},
		{Role: appmedia.AudioRoleEffect, Path: "/tmp/hit.m4a"},
		{Role: appmedia.AudioRoleDialogue, Path: "/tmp/line-b.m4a"},
	}}.Normalized()

	graph, err := audioMixGraph(mix)
	if err != nil {
		t.Fatal(err)
	}
	// The graph preserves the caller's order — the ORDERING BY ROLE is the export service's job, and
	// this test states that division: if the graph reordered, the caller's numbering and ffmpeg's
	// would disagree.
	if strings.Index(graph, "[1:a:0]") > strings.Index(graph, "[2:a:0]") {
		t.Fatalf("the graph reordered its inputs: %s", graph)
	}
}
