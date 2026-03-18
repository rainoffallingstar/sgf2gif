package main

import (
	"strings"
	"testing"

	"github.com/toikarin/sgf"
)

func TestAnnotatedSGFForVariationRoundTripsCachedAnalysis(t *testing.T) {
	collection, err := sgf.ParseSgf("(;FF[4]GM[1]SZ[9]PB[Black]PW[White];B[aa];W[bb])")
	if err != nil {
		t.Fatalf("ParseSgf returned error: %v", err)
	}

	analysis := &analysisSeries{
		frames: []positionAnalysis{
			{
				winrate:    0.60,
				scoreLead:  2.5,
				visits:     100,
				playedMove: "A9",
				bestMove:   "D4",
				moveLoss:   0.7,
				lossKnown:  true,
				topMoves: []analysisMove{
					{move: "D4", x: 3, y: 5, visits: 100, order: 0, winrate: 0.60, scoreLead: 2.5},
				},
			},
			{
				winrate:    0.58,
				scoreLead:  1.8,
				visits:     90,
				playedMove: "B8",
				bestMove:   "C3",
				moveLoss:   0.4,
				lossKnown:  true,
			},
		},
	}

	data, err := annotatedSGFForVariation(collection, 9, nil, analysis)
	if err != nil {
		t.Fatalf("annotatedSGFForVariation returned error: %v", err)
	}
	if !strings.Contains(string(data), katagoCacheRootProp+"["+katagoCacheVersion+"]") {
		t.Fatalf("annotated SGF missing root cache property: %s", string(data))
	}

	cachedCollection, err := sgf.ParseSgf(string(data))
	if err != nil {
		t.Fatalf("ParseSgf cached output returned error: %v", err)
	}
	cachedGame, err := firstGame(cachedCollection)
	if err != nil {
		t.Fatalf("firstGame cached output returned error: %v", err)
	}
	loaded, err := cachedAnalysisFromGame(cachedGame, 9, nil)
	if err != nil {
		t.Fatalf("cachedAnalysisFromGame returned error: %v", err)
	}
	if loaded == nil || len(loaded.frames) != 2 {
		t.Fatalf("loaded frames = %#v", loaded)
	}
	if loaded.frames[0].bestMove != "D4" {
		t.Fatalf("bestMove = %q, want %q", loaded.frames[0].bestMove, "D4")
	}
	if !loaded.frames[0].lossKnown || loaded.frames[0].moveLoss != 0.7 {
		t.Fatalf("unexpected cached loss: %#v", loaded.frames[0])
	}
	if loaded.frames[1].playedMove != "B8" {
		t.Fatalf("playedMove = %q, want %q", loaded.frames[1].playedMove, "B8")
	}
}

func TestAnnotatedSGFPath(t *testing.T) {
	if got := annotatedSGFPath("/tmp/out.gif"); got != "/tmp/out.katago.sgf" {
		t.Fatalf("annotatedSGFPath = %q", got)
	}
}
