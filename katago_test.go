package main

import (
	"math"
	"os"
	"testing"
)

func TestParseArgsAcceptsKataGoFlags(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{
		"sgf2gif",
		"--katago-strength", "strong",
		"--katago-bin", "/tmp/katago",
		"--katago-model", "/tmp/model.bin.gz",
		"--katago-config", "/tmp/analysis.cfg",
		"--katago-threads", "4",
		"--katago-top-moves", "5",
		"in.sgf",
		"out.gif",
	}

	opts, err := parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}

	if !opts.enableKataGo {
		t.Fatal("enableKataGo = false, want true")
	}
	if opts.katagoStrength != "strong" {
		t.Fatalf("katagoStrength = %q, want %q", opts.katagoStrength, "strong")
	}
	if opts.katagoVisits != 1000 || opts.katagoThreads != 4 || opts.katagoTopMoves != 5 {
		t.Fatalf("unexpected KataGo numeric flags: %#v", opts)
	}
}

func TestParseArgsExplicitVisitsOverrideStrength(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{
		"sgf2gif",
		"--katago-strength", "monster",
		"--katago-visits", "7",
		"in.sgf",
		"out.gif",
	}

	opts, err := parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if opts.katagoVisits != 7 {
		t.Fatalf("katagoVisits = %d, want 7", opts.katagoVisits)
	}
}

func TestParseArgsAcceptsKataGoViewFlag(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{
		"sgf2gif",
		"--katago-view", "white",
		"in.sgf",
		"out.gif",
	}

	opts, err := parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if !opts.enableKataGo {
		t.Fatal("enableKataGo = false, want true when katago-view is set")
	}
	if opts.katagoView != "white" {
		t.Fatalf("katagoView = %q, want %q", opts.katagoView, "white")
	}
}

func TestNormalizeKataGoView(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "", want: "black"},
		{input: "black", want: "black"},
		{input: "WHITE", want: "white"},
		{input: "  white  ", want: "white"},
		{input: "both", wantErr: true},
	}

	for _, tt := range tests {
		got, err := normalizeKataGoView(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("normalizeKataGoView(%q) returned nil error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizeKataGoView(%q) returned error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("normalizeKataGoView(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestKataGoVisitsForStrength(t *testing.T) {
	tests := map[string]int{
		"mild":    50,
		"fast":    100,
		"strong":  1000,
		"monster": 10000,
	}
	for input, want := range tests {
		got, err := katagoVisitsForStrength(input)
		if err != nil {
			t.Fatalf("katagoVisitsForStrength(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("katagoVisitsForStrength(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestSelectKataGoAssetPrefersEigen(t *testing.T) {
	release := &githubRelease{
		TagName: "v1.16.4",
		Assets: []githubReleaseAsset{
			{Name: "katago-v1.16.4-opencl-linux-x64.zip", URL: "opencl"},
			{Name: "katago-v1.16.4-eigenavx2-linux-x64.zip", URL: "eigenavx2"},
		},
	}

	asset, err := selectKataGoAsset(release, "linux", "amd64")
	if err != nil {
		t.Fatalf("selectKataGoAsset returned error: %v", err)
	}
	if asset.URL != "eigenavx2" {
		t.Fatalf("selected asset URL = %q, want %q", asset.URL, "eigenavx2")
	}
}

func TestExtractModelURLsPrefersLatestUniqueLinks(t *testing.T) {
	html := `
		<a href="https://media.katagotraining.org/uploaded/networks/models/kata1-b18c384nbt-latest.bin.gz">b18</a>
		<a href="https://media.katagotraining.org/uploaded/networks/models/kata1-b18c384nbt-latest.bin.gz">dup</a>
		<a href="https://media.katagotraining.org/uploaded/networks/models/kata1-b28c512nbt-latest.bin.gz">b28</a>
	`

	urls := extractModelURLs(html)
	if len(urls) != 2 {
		t.Fatalf("got %d model urls, want 2", len(urls))
	}
	if urls[0] != "https://media.katagotraining.org/uploaded/networks/models/kata1-b18c384nbt-latest.bin.gz" {
		t.Fatalf("unexpected first model url: %q", urls[0])
	}
}

func TestParseGTPMove(t *testing.T) {
	x, y, pass, err := parseGTPMove("Q16", 19)
	if err != nil {
		t.Fatalf("parseGTPMove returned error: %v", err)
	}
	if pass || x != 15 || y != 3 {
		t.Fatalf("unexpected parsed move: x=%d y=%d pass=%v", x, y, pass)
	}
}

func TestSelectRenderLayoutAddsAnalysisPanel(t *testing.T) {
	layout := selectRenderLayout(nil, true)
	if layout.analysisHeight != analysisHeight {
		t.Fatalf("analysisHeight = %d, want %d", layout.analysisHeight, analysisHeight)
	}
}

func TestBuildKataGoQueriesAddsDecisionQueriesForPlayedMoves(t *testing.T) {
	initial := newBoardState(9)
	actions := []*action{
		{move: &move{x: 0, y: 0}},
		{comment: "note only"},
		{move: &move{white: true, x: 1, y: 1}},
	}

	specs, err := actionsToFrameSpecs(initial, actions, positionalSuperkoRule)
	if err != nil {
		t.Fatalf("actionsToFrameSpecs returned error: %v", err)
	}

	queries, decisions := buildKataGoQueries(&gameInfo{rules: "Chinese"}, specs, 321)
	if len(queries) != 5 {
		t.Fatalf("got %d queries, want 5", len(queries))
	}
	if len(decisions) != 2 {
		t.Fatalf("got %d decision queries, want 2", len(decisions))
	}

	ids := map[string]bool{}
	for _, query := range queries {
		ids[query.ID] = true
	}
	for _, wantID := range []string{"frame-0000", "frame-0001", "frame-0002", "decision-0000", "decision-0002"} {
		if !ids[wantID] {
			t.Fatalf("missing query id %q in %#v", wantID, ids)
		}
	}

	if got := decisions[0].id(); got != "decision-0000" {
		t.Fatalf("first decision id = %q, want %q", got, "decision-0000")
	}
	if got := moveToGTP(decisions[0].move, decisions[0].before.size); got != "A9" {
		t.Fatalf("first played move = %q, want %q", got, "A9")
	}
	if decisions[0].before.toPlay != black {
		t.Fatalf("first decision toPlay = %v, want black", decisions[0].before.toPlay)
	}

	if got := decisions[1].id(); got != "decision-0002" {
		t.Fatalf("second decision id = %q, want %q", got, "decision-0002")
	}
	if got := moveToGTP(decisions[1].move, decisions[1].before.size); got != "B8" {
		t.Fatalf("second played move = %q, want %q", got, "B8")
	}
	if decisions[1].before.toPlay != white {
		t.Fatalf("second decision toPlay = %v, want white", decisions[1].before.toPlay)
	}
}

func TestPopulateAnalysisFramesIgnoresDecisionResponsesForFrameIndexing(t *testing.T) {
	specs := []frameSpec{
		{state: newBoardState(9)},
	}
	results := map[string]katagoAnalysisResponse{
		frameQueryID(0): {
			ID: frameQueryID(0),
			RootInfo: katagoRootInfo{
				Winrate:   0.61,
				ScoreLead: 2.5,
				Visits:    100,
			},
			MoveInfos: []katagoMoveInfo{
				{Move: "D4", Visits: 90, Order: 0, Winrate: 0.61, ScoreLead: 2.5},
			},
		},
		"decision-0000": {
			ID: "decision-0000",
			RootInfo: katagoRootInfo{
				Winrate:   0.55,
				ScoreLead: 1.0,
				Visits:    100,
			},
			MoveInfos: []katagoMoveInfo{
				{Move: "E5", Visits: 80, Order: 0, Winrate: 0.55, ScoreLead: 1.0},
			},
		},
	}

	frames, err := populateAnalysisFrames(specs, results, katagoOptions{topMoves: 3})
	if err != nil {
		t.Fatalf("populateAnalysisFrames returned error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if frames[0].visits != 100 {
		t.Fatalf("visits = %d, want 100", frames[0].visits)
	}
	if len(frames[0].topMoves) != 1 {
		t.Fatalf("got %d top moves, want 1", len(frames[0].topMoves))
	}
	if frames[0].topMoves[0].move != "D4" {
		t.Fatalf("top move = %q, want %q", frames[0].topMoves[0].move, "D4")
	}
}

func TestRootScoreForPlayer(t *testing.T) {
	if got := rootScoreForPlayer(3.5, black); got != 3.5 {
		t.Fatalf("rootScoreForPlayer(3.5, black) = %v, want 3.5", got)
	}
	if got := rootScoreForPlayer(3.5, white); got != -3.5 {
		t.Fatalf("rootScoreForPlayer(3.5, white) = %v, want -3.5", got)
	}
}

func TestApplyDecisionAnalysisUsesFrameRootForActualMoveLoss(t *testing.T) {
	frames := []positionAnalysis{
		{scoreLead: 1.2},
	}
	decisions := []decisionQueryRef{
		{
			frameIndex: 0,
			before:     newBoardState(9),
			move:       &move{x: 0, y: 0},
		},
	}
	results := map[string]katagoAnalysisResponse{
		"decision-0000": {
			MoveInfos: []katagoMoveInfo{
				{Move: "D4", ScoreLead: 2.7, Visits: 100, Order: 0},
			},
		},
	}

	applyDecisionAnalysis(frames, decisions, results)

	if frames[0].playedMove != "A9" {
		t.Fatalf("playedMove = %q, want %q", frames[0].playedMove, "A9")
	}
	if frames[0].bestMove != "D4" {
		t.Fatalf("bestMove = %q, want %q", frames[0].bestMove, "D4")
	}
	if !frames[0].lossKnown {
		t.Fatal("lossKnown = false, want true")
	}
	if math.Abs(frames[0].moveLoss-1.5) > 1e-9 {
		t.Fatalf("moveLoss = %v, want 1.5", frames[0].moveLoss)
	}
}
