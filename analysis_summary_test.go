package main

import (
	"strings"
	"testing"
)

func TestBuildAnalysisSummaryCountsPhasesAndCategories(t *testing.T) {
	actions := []*action{
		{move: &move{x: 0, y: 0}},
		{move: &move{white: true, x: 1, y: 1}},
		{comment: "note"},
		{move: &move{x: 2, y: 2}},
		{move: &move{white: true, x: 3, y: 3}},
		{move: &move{x: 4, y: 4}},
		{move: &move{white: true, x: 5, y: 5}},
	}
	analysis := &analysisSeries{
		frames: []positionAnalysis{
			{playedMove: "A9", topMoves: []analysisMove{{move: "A9"}}, winrateGap: -0.02, lossKnown: true, moveLoss: -0.2},
			{playedMove: "B8", topMoves: []analysisMove{{move: "C7"}}, winrateGap: 0.02, lossKnown: true, moveLoss: 0.4},
			{},
			{playedMove: "C7", topMoves: []analysisMove{{move: "C7"}}, winrateGap: 0.08, lossKnown: true, moveLoss: 1.8},
			{playedMove: "D6", topMoves: []analysisMove{{move: "E5"}}, winrateGap: 0.15, lossKnown: true, moveLoss: 3.2},
			{playedMove: "E5", topMoves: []analysisMove{{move: "E5"}}, winrateGap: 0.01, lossKnown: true, moveLoss: 0.1},
			{playedMove: "F4", topMoves: []analysisMove{{move: "G3"}}, winrateGap: 0.25, lossKnown: true, moveLoss: 5.5},
		},
	}

	specs := []frameSpec{
		{current: actions[0], moveNumber: 1},
		{current: actions[1], moveNumber: 2},
		{current: actions[2], moveNumber: 2},
		{current: actions[3], moveNumber: 3},
		{current: actions[4], moveNumber: 4},
		{current: actions[5], moveNumber: 5},
		{current: actions[6], moveNumber: 6},
	}

	summary := buildAnalysisSummaryFromSpecs(specs, analysis)
	if summary == nil {
		t.Fatal("buildAnalysisSummary returned nil")
	}
	if summary.totalMoves != 6 {
		t.Fatalf("totalMoves = %d, want 6", summary.totalMoves)
	}
	if summary.topMoveCount != 1 {
		t.Fatalf("topMoveCount = %d, want 1", summary.topMoveCount)
	}
	if summary.phases[0].black.hits != 1 || summary.phases[0].black.total != 1 {
		t.Fatalf("unexpected opening black stats: %#v", summary.phases[0].black)
	}
	if summary.phases[0].white.hits != 0 || summary.phases[0].white.total != 1 {
		t.Fatalf("unexpected opening white stats: %#v", summary.phases[0].white)
	}
	if summary.categories[0].black != 1 {
		t.Fatalf("super black count = %d, want 1", summary.categories[0].black)
	}
	if summary.categories[2].white != 1 {
		t.Fatalf("playable white count = %d, want 1", summary.categories[2].white)
	}
	if summary.categories[4].black != 1 {
		t.Fatalf("mistake black count = %d, want 1", summary.categories[4].black)
	}
	if summary.categories[6].white != 1 {
		t.Fatalf("blunder white count = %d, want 1", summary.categories[6].white)
	}
	if summary.blackHitStreak != 3 {
		t.Fatalf("blackHitStreak = %d, want 3", summary.blackHitStreak)
	}
	if summary.whiteHitStreak != 0 {
		t.Fatalf("whiteHitStreak = %d, want 0", summary.whiteHitStreak)
	}
	if summary.blackLoss.average() <= 0 || summary.whiteLoss.average() <= 0 {
		t.Fatalf("expected positive average loss stats, got black=%f white=%f", summary.blackLoss.average(), summary.whiteLoss.average())
	}
	if !summary.worstWhite.valid || summary.worstWhite.move != "F4" {
		t.Fatalf("unexpected worst white move: %#v", summary.worstWhite)
	}
	if !summary.bestBlackPhase.valid || summary.bestBlackPhase.label != "Opening" {
		t.Fatalf("unexpected best black phase: %#v", summary.bestBlackPhase)
	}
	if !summary.bestWhitePhase.valid || summary.bestWhitePhase.label != "Opening" {
		t.Fatalf("unexpected best white phase: %#v", summary.bestWhitePhase)
	}
	if !summary.worstBlackPhase.valid || summary.worstBlackPhase.label != "Opening" {
		t.Fatalf("unexpected worst black phase: %#v", summary.worstBlackPhase)
	}
	if !summary.worstWhitePhase.valid || summary.worstWhitePhase.label != "Opening" {
		t.Fatalf("unexpected worst white phase: %#v", summary.worstWhitePhase)
	}
	if !summary.biggestSwing.valid || summary.biggestSwing.move != "F4" {
		t.Fatalf("unexpected biggest swing: %#v", summary.biggestSwing)
	}
	if len(summary.topBlunders) == 0 || summary.topBlunders[0].move != "F4" {
		t.Fatalf("unexpected top blunders: %#v", summary.topBlunders)
	}
	if verdict := verdictForSide(summary, false); !strings.Contains(verdict, "Black") {
		t.Fatalf("unexpected black verdict: %q", verdict)
	}
	if verdict := verdictForSide(summary, true); !strings.Contains(verdict, "White") {
		t.Fatalf("unexpected white verdict: %q", verdict)
	}
	if verdict := phasePressureVerdict(summary); !strings.Contains(verdict, "Phase pressure") {
		t.Fatalf("unexpected phase pressure verdict: %q", verdict)
	}
}

func TestRecommendationHitPrefersPlayedHitWhenKnown(t *testing.T) {
	frame := positionAnalysis{
		playedMove:     "A9",
		playedHit:      true,
		playedHitKnown: true,
		topMoves:       []analysisMove{{move: "B8"}},
	}
	if !frame.recommendationHit() {
		t.Fatal("expected recommendationHit=true when playedHit is known true")
	}

	frame.playedHit = false
	if frame.recommendationHit() {
		t.Fatal("expected recommendationHit=false when playedHit is known false")
	}
}

func TestBuildAnalysisSummaryReturnsSummaryWhenNoMoves(t *testing.T) {
	specs := []frameSpec{{current: &action{comment: "diagram"}}}
	analysis := &analysisSeries{frames: []positionAnalysis{{topMoves: []analysisMove{{move: "D4"}}}}}
	summary := buildAnalysisSummaryFromSpecs(specs, analysis)
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if summary.totalMoves != 0 {
		t.Fatalf("totalMoves = %d, want 0", summary.totalMoves)
	}
}
