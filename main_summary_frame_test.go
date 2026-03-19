package main

import "testing"

func TestActionsToFramesAddsSummaryFrameWhenSummaryMissing(t *testing.T) {
	info := &gameInfo{}
	initial := newBoardState(19)
	actions := []*action{
		{move: &move{x: 3, y: 3, white: false}},
	}

	specs, err := actionsToFrameSpecs(initial, actions, positionalSuperkoRule)
	if err != nil {
		t.Fatalf("actionsToFrameSpecs: %v", err)
	}
	if len(specs) == 0 {
		t.Fatal("expected non-empty specs")
	}

	analysis := &analysisSeries{
		frames: make([]positionAnalysis, len(specs)),
	}

	cfg := renderConfig{
		layout:   selectRenderLayout(actions, true),
		analysis: analysis,
	}

	frames, err := actionsToFrames(info, initial, actions, cfg, positionalSuperkoRule)
	if err != nil {
		t.Fatalf("actionsToFrames: %v", err)
	}

	want := len(specs) + 1
	if len(frames) != want {
		t.Fatalf("frames=%d, want %d (summary frame appended)", len(frames), want)
	}
}
