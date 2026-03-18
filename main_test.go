package main

import (
	"image"
	"image/gif"
	"os"
	"path/filepath"
	"testing"

	"github.com/toikarin/sgf"
)

func TestMovesFromGameFollowsFirstChildLine(t *testing.T) {
	game := mustParseGameTree(t, "(;FF[4]GM[1]SZ[9];B[aa](;W[bb](;B[]))(;W[cc]))")

	moves, err := movesFromGame(game, 9)
	if err != nil {
		t.Fatalf("movesFromGame returned error: %v", err)
	}

	if len(moves) != 3 {
		t.Fatalf("got %d moves, want 3", len(moves))
	}

	if moves[0].white || moves[0].pass || moves[0].x != 0 || moves[0].y != 0 {
		t.Fatalf("unexpected first move: %#v", moves[0])
	}

	if !moves[1].white || moves[1].pass || moves[1].x != 1 || moves[1].y != 1 {
		t.Fatalf("unexpected second move: %#v", moves[1])
	}

	if moves[2].white || !moves[2].pass {
		t.Fatalf("unexpected pass move: %#v", moves[2])
	}
}

func TestSaveReturnsOpenErrorInsteadOfPanicking(t *testing.T) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("save panicked: %v", r)
		}
	}()

	err := save(filepath.Join(t.TempDir(), "missing", "out.gif"), &gif.GIF{})
	if err == nil {
		t.Fatal("save returned nil error for missing directory")
	}
}

func TestSgfToGifRespectsBoardSizeAndPasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game.sgf")
	content := "(;FF[4]GM[1]SZ[9]PB[Black]PW[White];B[aa];W[];B[bi])"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	g, err := sgfToGif(&options{inputPath: path})
	if err != nil {
		t.Fatalf("sgfToGif returned error: %v", err)
	}

	if len(g.Image) != 3 {
		t.Fatalf("got %d frames, want 3", len(g.Image))
	}

	if got, want := g.Image[0].Bounds().Dx(), side(9)+2*coordMargin; got != want {
		t.Fatalf("first frame width = %d, want %d", got, want)
	}

	if got, want := g.Image[1].Bounds().Dy(), compactInfoHeight+side(9)+2*coordMargin; got != want {
		t.Fatalf("second frame height = %d, want %d", got, want)
	}
}

func TestSgfToGifKeepsFullHeightWhenCommentsExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "game-with-comments.sgf")
	content := "(;FF[4]GM[1]SZ[9]PB[Black]PW[White];B[aa]C[opening note];W[bi])"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	g, err := sgfToGif(&options{inputPath: path})
	if err != nil {
		t.Fatalf("sgfToGif returned error: %v", err)
	}

	if got, want := g.Image[0].Bounds().Dy(), infoHeight+side(9)+2*coordMargin; got != want {
		t.Fatalf("frame height = %d, want %d", got, want)
	}
}

func TestActionsToFramesAppendsSummaryFrameWhenAnalysisExists(t *testing.T) {
	initial := newBoardState(9)
	actions := []*action{
		{move: &move{x: 0, y: 0}},
		{move: &move{white: true, x: 1, y: 1}},
	}
	cfg := renderConfig{
		layout: renderLayout{infoHeight: compactInfoHeight, analysisHeight: analysisHeight},
		analysis: &analysisSeries{
			frames: []positionAnalysis{
				{playedMove: "A9", bestMove: "B8", lossKnown: true, winrateGap: 0.02, topMoves: []analysisMove{{move: "B8", x: 1, y: 1}}},
				{playedMove: "B8", bestMove: "C7", lossKnown: true, winrateGap: 0.04, topMoves: []analysisMove{{move: "C7", x: 2, y: 2}}},
			},
			summary: &analysisSummary{
				phases: []phaseSummary{{label: "Opening"}},
			},
		},
	}

	frames, err := actionsToFrames(&gameInfo{}, initial, actions, cfg, positionalSuperkoRule)
	if err != nil {
		t.Fatalf("actionsToFrames returned error: %v", err)
	}
	if got, want := len(frames), 3; got != want {
		t.Fatalf("frame count = %d, want %d", got, want)
	}
	if got, want := frames[len(frames)-1].Bounds().Dy(), compactInfoHeight+side(9)+2*coordMargin+analysisHeight; got != want {
		t.Fatalf("summary frame height = %d, want %d", got, want)
	}
}

func TestParseArgsAcceptsMoveNumbersFlag(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{"sgf2gif", "--move-numbers", "in.sgf", "out.gif"}
	opts, err := parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}

	if !opts.showMoveNumbers {
		t.Fatal("showMoveNumbers = false, want true")
	}
	if opts.inputPath != "in.sgf" || opts.outputPath != "out.gif" {
		t.Fatalf("unexpected parsed paths: %#v", opts)
	}
}

func TestParseArgsAcceptsRecentMoveNumbersFlag(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{"sgf2gif", "--recent-move-numbers", "12", "in.sgf", "out.gif"}
	opts, err := parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}

	if opts.recentMoves != 12 {
		t.Fatalf("recentMoves = %d, want 12", opts.recentMoves)
	}
}

func TestParseArgsAcceptsVariationPathFlag(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{"sgf2gif", "--variation-path", "2,1", "in.sgf", "out.gif"}
	opts, err := parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}

	if len(opts.variationPath) != 2 || opts.variationPath[0] != 2 || opts.variationPath[1] != 1 {
		t.Fatalf("unexpected variation path: %#v", opts.variationPath)
	}
}

func TestParseArgsAcceptsAllVariationsFlag(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{"sgf2gif", "--all-variations", "in.sgf", "out.gif"}
	opts, err := parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if !opts.allVariations {
		t.Fatal("allVariations = false, want true")
	}
}

func TestParseArgsRejectsNegativeRecentMoveNumbers(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{"sgf2gif", "--recent-move-numbers", "-1", "in.sgf", "out.gif"}
	_, err := parseArgs()
	if err == nil {
		t.Fatal("expected parseArgs to reject negative recent move numbers")
	}
}

func TestParseArgsRejectsVariationPathWithAllVariations(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{"sgf2gif", "--all-variations", "--variation-path", "1", "in.sgf", "out.gif"}
	_, err := parseArgs()
	if err == nil {
		t.Fatal("expected parseArgs to reject combined all-variations and variation-path")
	}
}

func TestParseVariationPathRejectsInvalidValue(t *testing.T) {
	_, err := parseVariationPath("1,0")
	if err == nil {
		t.Fatal("expected parseVariationPath to reject non-positive entries")
	}
}

func TestVariationLabelFormatsPath(t *testing.T) {
	if got := variationLabel(nil); got != "Main line" {
		t.Fatalf("variationLabel(nil) = %q, want %q", got, "Main line")
	}
	if got := variationLabel([]int{2, 1}); got != "Variation 2.1" {
		t.Fatalf("variationLabel([2 1]) = %q, want %q", got, "Variation 2.1")
	}
}

func TestCollectVariationPathsSkipsSingleChildLevels(t *testing.T) {
	game := mustParseGameTree(t, "(;FF[4]GM[1];B[aa];W[bb](;B[cc])(;B[dd](;W[ee])(;W[ff])))")

	paths := collectVariationPaths(game)
	if len(paths) != 3 {
		t.Fatalf("got %d paths, want 3", len(paths))
	}
	if got := variationLabel(paths[0]); got != "Variation 1" {
		t.Fatalf("first path label = %q, want %q", got, "Variation 1")
	}
	if got := variationLabel(paths[2]); got != "Variation 2.2" {
		t.Fatalf("third path label = %q, want %q", got, "Variation 2.2")
	}
}

func TestVariationOutputPathAddsSuffixesForBatch(t *testing.T) {
	if got := variationOutputPath("/tmp/out.gif", []int{2, 1}, 3); got != "/tmp/out.var-2-1.gif" {
		t.Fatalf("variationOutputPath = %q", got)
	}
	if got := variationOutputPath("/tmp/out.gif", nil, 1); got != "/tmp/out.gif" {
		t.Fatalf("variationOutputPath single = %q", got)
	}
}

func TestKoRuleFromGameUsesRulesProperty(t *testing.T) {
	japanese := mustParseGameTree(t, "(;FF[4]GM[1]RU[Japanese])")
	rule, err := koRuleFromGame(japanese)
	if err != nil {
		t.Fatalf("koRuleFromGame returned error: %v", err)
	}
	if rule != simpleKoRule {
		t.Fatalf("rule = %v, want simpleKoRule", rule)
	}

	chinese := mustParseGameTree(t, "(;FF[4]GM[1]RU[Chinese])")
	rule, err = koRuleFromGame(chinese)
	if err != nil {
		t.Fatalf("koRuleFromGame returned error: %v", err)
	}
	if rule != positionalSuperkoRule {
		t.Fatalf("rule = %v, want positionalSuperkoRule", rule)
	}
}

func TestActionsFromGameIncludesSetupNodesAndMoveNumber(t *testing.T) {
	game := mustParseGameTree(t, "(;FF[4]GM[1]SZ[5]AB[aa][bb]PL[W];AE[aa];B[cc]MN[42])")

	actions, err := actionsFromGame(game, 5, nil)
	if err != nil {
		t.Fatalf("actionsFromGame returned error: %v", err)
	}

	if len(actions) != 3 {
		t.Fatalf("got %d actions, want 3", len(actions))
	}
	if actions[0].move != nil || len(actions[0].setups) != 2 || actions[0].toPlay != white {
		t.Fatalf("unexpected root action: %#v", actions[0])
	}
	if actions[1].move != nil || len(actions[1].setups) != 1 || actions[1].setups[0].stone != background {
		t.Fatalf("unexpected setup action: %#v", actions[1])
	}
	if actions[2].move == nil || actions[2].moveNumber != 42 {
		t.Fatalf("unexpected move action: %#v", actions[2])
	}
}

func TestActionsFromGameUsesVariationPath(t *testing.T) {
	game := mustParseGameTree(t, "(;FF[4]GM[1]SZ[9];B[aa](;W[bb])(;W[cc](;B[dd])(;B[ee])))")

	actions, err := actionsFromGame(game, 9, []int{2, 2})
	if err != nil {
		t.Fatalf("actionsFromGame returned error: %v", err)
	}

	if len(actions) != 3 {
		t.Fatalf("got %d actions, want 3", len(actions))
	}
	if actions[1].move == nil || actions[1].move.x != 2 || actions[1].move.y != 2 {
		t.Fatalf("unexpected second action: %#v", actions[1])
	}
	if actions[2].move == nil || actions[2].move.x != 4 || actions[2].move.y != 4 {
		t.Fatalf("unexpected third action: %#v", actions[2])
	}
}

func TestActionsFromGameVariationPathCountsOnlyBranches(t *testing.T) {
	game := mustParseGameTree(t, "(;FF[4]GM[1]SZ[9];B[aa];W[bb](;B[cc])(;B[dd]))")

	actions, err := actionsFromGame(game, 9, []int{2})
	if err != nil {
		t.Fatalf("actionsFromGame returned error: %v", err)
	}

	if len(actions) != 3 {
		t.Fatalf("got %d actions, want 3", len(actions))
	}
	if actions[2].move == nil || actions[2].move.x != 3 || actions[2].move.y != 3 {
		t.Fatalf("unexpected branch move: %#v", actions[2])
	}
}

func TestActionFromNodeParsesMarkersLabelsAndComment(t *testing.T) {
	node := &sgf.Node{
		Properties: []*sgf.Property{
			{Ident: "TR", Values: []string{"aa"}},
			{Ident: "SQ", Values: []string{"bb"}},
			{Ident: "CR", Values: []string{"cc"}},
			{Ident: "MA", Values: []string{"dd"}},
			{Ident: "SL", Values: []string{"ee"}},
			{Ident: "TB", Values: []string{"ff"}},
			{Ident: "TW", Values: []string{"gg"}},
			{Ident: "LB", Values: []string{"ee:A"}},
			{Ident: "C", Values: []string{"shape comment"}},
		},
	}

	action, hasEffect, err := actionFromNode(node, 9)
	if err != nil {
		t.Fatalf("actionFromNode returned error: %v", err)
	}
	if !hasEffect {
		t.Fatal("expected node annotations to count as an effect")
	}
	if len(action.marks) != 7 {
		t.Fatalf("got %d marks, want 7", len(action.marks))
	}
	if len(action.labels) != 1 || action.labels[0].text != "A" {
		t.Fatalf("unexpected labels: %#v", action.labels)
	}
	if action.comment != "shape comment" {
		t.Fatalf("comment = %q, want %q", action.comment, "shape comment")
	}
}

func TestWrapTextSplitsLongComment(t *testing.T) {
	lines := wrapText("Comment: this is a longer comment that should wrap onto multiple lines cleanly", 120, 2)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if lines[0] == "" || lines[1] == "" {
		t.Fatalf("unexpected wrapped lines: %#v", lines)
	}
}

func TestParseBoardEditsExpandsCompressedPointList(t *testing.T) {
	node := &sgf.Node{
		Properties: []*sgf.Property{
			{Ident: "AB", Values: []string{"aa:bb"}},
		},
	}

	edits, err := parseBoardEdits(node, "AB", black, 5)
	if err != nil {
		t.Fatalf("parseBoardEdits returned error: %v", err)
	}
	if len(edits) != 4 {
		t.Fatalf("got %d edits, want 4", len(edits))
	}
}

func TestActionsToFramesRespectsPlayerToPlaySetup(t *testing.T) {
	info := &gameInfo{}
	initial := newBoardState(5)
	actions := []*action{
		{toPlay: white},
	}

	frames, err := actionsToFrames(info, initial, actions, renderConfig{}, simpleKoRule)
	if err != nil {
		t.Fatalf("actionsToFrames returned error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if initial.toPlay != black {
		t.Fatalf("initial state should not be mutated, got toPlay=%d", initial.toPlay)
	}
}

func TestActionsToFramesCreatesFrameForCommentOnlyNode(t *testing.T) {
	info := &gameInfo{}
	initial := newBoardState(5)
	actions := []*action{
		{comment: "just a note"},
	}

	frames, err := actionsToFrames(info, initial, actions, renderConfig{}, simpleKoRule)
	if err != nil {
		t.Fatalf("actionsToFrames returned error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
}

func TestParseBoardSizeRejectsRectangularBoards(t *testing.T) {
	_, err := parseBoardSize("9:13")
	if err == nil {
		t.Fatal("parseBoardSize accepted a rectangular board")
	}
}

func TestApplyMoveCapturesSingleStone(t *testing.T) {
	state := newBoardState(3)
	state.set(1, 1, white)
	state.set(1, 0, black)
	state.set(0, 1, black)
	state.set(1, 2, black)

	captured, err := state.applyMove(&move{x: 2, y: 1}, []string{state.hash()}, 5, simpleKoRule)
	if err != nil {
		t.Fatalf("applyMove returned error: %v", err)
	}
	if captured != 1 {
		t.Fatalf("captured = %d, want 1", captured)
	}

	if got := state.get(1, 1); got != background {
		t.Fatalf("captured stone still on board: got %d", got)
	}

	if got := state.capturesBlack; got != 1 {
		t.Fatalf("capturesBlack = %d, want 1", got)
	}
	if got := state.moveNumberAt(2, 1); got != 5 {
		t.Fatalf("move number at played stone = %d, want 5", got)
	}
}

func TestApplyMoveRejectsImmediateKoRecapture(t *testing.T) {
	state := newBoardState(3)
	state.set(1, 0, black)
	state.set(2, 0, white)
	state.set(0, 1, black)
	state.set(1, 1, white)
	state.set(1, 2, black)
	state.set(2, 2, white)

	history := []string{state.hash()}
	if _, err := state.applyMove(&move{x: 2, y: 1}, history, 7, simpleKoRule); err != nil {
		t.Fatalf("ko capture setup failed: %v", err)
	}
	history = append(history, state.hash())

	_, err := state.applyMove(&move{white: true, x: 1, y: 1}, history, 8, simpleKoRule)
	if err == nil {
		t.Fatal("expected ko recapture to fail")
	}
}

func TestApplyMoveRejectsPositionalSuperko(t *testing.T) {
	state := newBoardState(3)
	state.set(1, 1, white)
	state.set(1, 0, black)
	state.set(0, 1, black)
	state.set(1, 2, black)

	target := state.clone()
	if _, err := target.applyMove(&move{x: 2, y: 1}, []string{state.hash()}, 9, simpleKoRule); err != nil {
		t.Fatalf("failed to build target state: %v", err)
	}

	history := []string{target.hash(), "intermediate", state.hash()}
	_, err := state.applyMove(&move{x: 2, y: 1}, history, 9, positionalSuperkoRule)
	if err == nil {
		t.Fatal("expected positional superko to reject repeated board state")
	}
}

func TestStarPointsForNineBoard(t *testing.T) {
	points := starPoints(9)
	if len(points) != 9 {
		t.Fatalf("got %d star points, want 9", len(points))
	}

	want := point{x: 4, y: 4}
	found := false
	for _, p := range points {
		if p == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing center star point: %#v", want)
	}
}

func TestDrawStoneMoveNumbersHonorsRecentWindow(t *testing.T) {
	state := newBoardState(9)
	state.set(0, 0, black)
	state.setMoveNumber(0, 0, 2)
	state.set(1, 0, white)
	state.setMoveNumber(1, 0, 9)

	img := image.NewPaletted(image.Rect(0, 0, side(9)+2*coordMargin, infoHeight+side(9)+2*coordMargin), palette)
	fill(img, background)

	drawStoneMoveNumbers(img, state, 10, false, 3)

	oldX := boardOriginX()
	oldY := boardOriginY() + 5
	newX := boardOriginX() + stoneDiameter
	newY := boardOriginY() + 5

	if hasNonBackgroundPixel(img, oldX-6, oldY-8, oldX+6, oldY+4) {
		t.Fatal("older move number should not be drawn")
	}
	if !hasNonBackgroundPixel(img, newX-6, newY-8, newX+6, newY+4) {
		t.Fatal("recent move number should be drawn")
	}
}

func hasNonBackgroundPixel(img *image.Paletted, minX, minY, maxX, maxY int) bool {
	bounds := img.Bounds()
	if minX < bounds.Min.X {
		minX = bounds.Min.X
	}
	if minY < bounds.Min.Y {
		minY = bounds.Min.Y
	}
	if maxX >= bounds.Max.X {
		maxX = bounds.Max.X - 1
	}
	if maxY >= bounds.Max.Y {
		maxY = bounds.Max.Y - 1
	}

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if img.ColorIndexAt(x, y) != background {
				return true
			}
		}
	}
	return false
}

func mustParseGameTree(t *testing.T, content string) *sgf.GameTree {
	t.Helper()

	c, err := sgf.ParseSgf(content)
	if err != nil {
		t.Fatalf("ParseSgf returned error: %v", err)
	}

	game, err := firstGame(c)
	if err != nil {
		t.Fatalf("firstGame returned error: %v", err)
	}

	return game
}
