package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"log"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/toikarin/sgf"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

type options struct {
	inputPath       string
	outputPath      string
	showMoveNumbers bool
	recentMoves     int
	variationPath   []int
	allVariations   bool
	enableKataGo    bool
	katagoBin       string
	katagoModel     string
	katagoConfig    string
	katagoStrength  string
	katagoView      string
	katagoVisits    int
	katagoThreads   int
	katagoTopMoves  int
}

type renderConfig struct {
	showMoveNumbers bool
	recentMoves     int
	variationLabel  string
	layout          renderLayout
	analysis        *analysisSeries
	currentFrame    int
	katagoView      string
}

type renderLayout struct {
	infoHeight     int
	analysisHeight int
}

var playerHeaderFace = mustLoadFontFace(gobold.TTF, 18)

func main() {
	opts, err := parseArgs()
	if err != nil {
		usage()
		log.Fatal(err)
	}

	outputs, err := sgfToGifs(opts)
	if err != nil {
		log.Fatal(err)
	}

	for _, out := range outputs {
		err = save(out.path, out.gif)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func parseArgs() (*options, error) {
	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	showMoveNumbers := fs.Bool("move-numbers", false, "draw move numbers on stones")
	recentMoves := fs.Int("recent-move-numbers", 0, "draw move numbers only for the most recent N moves")
	variationPath := fs.String("variation-path", "", "choose SGF variation path using 1-based indices separated by commas")
	allVariations := fs.Bool("all-variations", false, "export all leaf variations to separate GIF files")
	enableKataGo := fs.Bool("katago-analyze", false, "analyze each rendered position with KataGo")
	katagoBin := fs.String("katago-bin", "", "path to the KataGo executable")
	katagoModel := fs.String("katago-model", "", "path to the KataGo model (.bin.gz)")
	katagoConfig := fs.String("katago-config", "", "path to the KataGo analysis config (.cfg)")
	katagoStrength := fs.String("katago-strength", "", "KataGo strength preset: fast, strong, or monster")
	katagoView := fs.String("katago-view", "black", "KataGo display perspective: black or white")
	katagoVisits := fs.Int("katago-visits", 200, "maximum KataGo visits per rendered position")
	katagoThreads := fs.Int("katago-threads", 2, "number of KataGo analysis threads")
	katagoTopMoves := fs.Int("katago-top-moves", 3, "number of KataGo candidate moves to display on the board")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return nil, err
	}

	args := fs.Args()
	if len(args) != 2 {
		return nil, fmt.Errorf("bad number of arguments")
	}
	if *recentMoves < 0 {
		return nil, fmt.Errorf("recent-move-numbers must be non-negative")
	}
	if *allVariations && *variationPath != "" {
		return nil, fmt.Errorf("variation-path cannot be combined with all-variations")
	}
	if *katagoVisits <= 0 {
		return nil, fmt.Errorf("katago-visits must be positive")
	}
	if *katagoThreads <= 0 {
		return nil, fmt.Errorf("katago-threads must be positive")
	}
	if *katagoTopMoves < 0 {
		return nil, fmt.Errorf("katago-top-moves must be non-negative")
	}
	resolvedView, err := normalizeKataGoView(*katagoView)
	if err != nil {
		return nil, err
	}

	visited := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		visited[f.Name] = true
	})

	resolvedStrength := strings.ToLower(strings.TrimSpace(*katagoStrength))
	if resolvedStrength != "" {
		presetVisits, err := katagoVisitsForStrength(resolvedStrength)
		if err != nil {
			return nil, err
		}
		if !visited["katago-visits"] {
			*katagoVisits = presetVisits
		}
	}

	katagoConfigured := *enableKataGo ||
		visited["katago-bin"] ||
		visited["katago-model"] ||
		visited["katago-config"] ||
		visited["katago-strength"] ||
		visited["katago-view"] ||
		visited["katago-visits"] ||
		visited["katago-threads"] ||
		visited["katago-top-moves"]

	path, err := parseVariationPath(*variationPath)
	if err != nil {
		return nil, err
	}

	return &options{
		inputPath:       args[0],
		outputPath:      args[1],
		showMoveNumbers: *showMoveNumbers,
		recentMoves:     *recentMoves,
		variationPath:   path,
		allVariations:   *allVariations,
		enableKataGo:    katagoConfigured,
		katagoBin:       *katagoBin,
		katagoModel:     *katagoModel,
		katagoConfig:    *katagoConfig,
		katagoStrength:  resolvedStrength,
		katagoView:      resolvedView,
		katagoVisits:    *katagoVisits,
		katagoThreads:   *katagoThreads,
		katagoTopMoves:  *katagoTopMoves,
	}, nil
}

func save(path string, g *gif.GIF) (err error) {
	f, err := os.OpenFile(path,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	defer func() {
		errClose := f.Close()
		if err == nil {
			err = errClose
		}
	}()

	return gif.EncodeAll(f, g)
}

func usage() {
	log.Printf("usage: %s [--move-numbers] [--recent-move-numbers N] [--variation-path 2,1,...] [--all-variations] [--katago-analyze] [--katago-bin PATH] [--katago-model PATH] [--katago-config PATH] [--katago-strength fast|strong|monster] [--katago-view black|white] [--katago-visits N] [--katago-threads N] [--katago-top-moves N] input_sgf_file output_gif_file\n", os.Args[0])
}

type renderOutput struct {
	path string
	gif  *gif.GIF
}

func sgfToGif(opts *options) (*gif.GIF, error) {
	outputs, err := sgfToGifs(opts)
	if err != nil {
		return nil, err
	}
	if len(outputs) != 1 {
		return nil, fmt.Errorf("expected one GIF output, got %d", len(outputs))
	}
	return outputs[0].gif, nil
}

func sgfToGifs(opts *options) ([]renderOutput, error) {
	c, err := sgf.ParseSgfFile(opts.inputPath)
	if err != nil {
		return nil, err
	}

	game, err := firstGame(c)
	if err != nil {
		return nil, err
	}

	boardSize, err := boardSizeFromGame(game)
	if err != nil {
		return nil, err
	}

	info, err := gameInfoFromGame(game)
	if err != nil {
		return nil, err
	}

	initial, err := initialBoardFromGame(boardSize)
	if err != nil {
		return nil, err
	}

	koRule, err := koRuleFromGame(game)
	if err != nil {
		return nil, err
	}

	paths := [][]int{opts.variationPath}
	if opts.allVariations {
		paths = collectVariationPaths(game)
	}

	outputs := make([]renderOutput, 0, len(paths))
	for _, path := range paths {
		actions, err := actionsFromGame(game, boardSize, path)
		if err != nil {
			return nil, err
		}

		var analysis *analysisSeries
		if opts.enableKataGo {
			analysis, err = analyzeActionsWithKataGo(info, initial, actions, koRule, katagoOptionsFromCLI(opts))
			if err != nil {
				return nil, err
			}
		}

		cfg := renderConfig{
			showMoveNumbers: opts.showMoveNumbers,
			recentMoves:     opts.recentMoves,
			variationLabel:  variationLabel(path),
			layout:          selectRenderLayout(actions, analysis != nil),
			analysis:        analysis,
			katagoView:      opts.katagoView,
		}

		frames, err := actionsToFrames(info, initial, actions, cfg, koRule)
		if err != nil {
			return nil, err
		}

		g, err := framesToGif(frames)
		if err != nil {
			return nil, err
		}

		outputs = append(outputs, renderOutput{
			path: variationOutputPath(opts.outputPath, path, len(paths)),
			gif:  g,
		})
	}

	return outputs, nil
}

func firstGame(c *sgf.Collection) (*sgf.GameTree, error) {
	switch n := len(c.GameTrees); n {
	case 0:
		return nil, fmt.Errorf("no games in the file")
	case 1:
		break // OK
	default:
		log.Printf("found %d games: using the first, ignoring the rest", n)
	}

	return c.GameTrees[0], nil
}

type move struct {
	white bool
	pass  bool
	x     int
	y     int
}

type boardEdit struct {
	x     int
	y     int
	stone uint8
}

type markKind uint8

const (
	markCircle markKind = iota + 1
	markSquare
	markTriangle
	markCross
	markSelected
	markTerritoryBlack
	markTerritoryWhite
)

type boardMark struct {
	x    int
	y    int
	kind markKind
}

type boardLabel struct {
	x    int
	y    int
	text string
}

type action struct {
	move       *move
	setups     []boardEdit
	moveNumber int
	toPlay     uint8
	marks      []boardMark
	labels     []boardLabel
	comment    string
}

type gameInfo struct {
	gameName  string
	date      string
	result    string
	rules     string
	komi      string
	handicap  string
	blackName string
	blackRank string
	whiteName string
	whiteRank string
}

type koRule int

const (
	simpleKoRule koRule = iota
	positionalSuperkoRule
)

func notAMove(p *sgf.Property) bool {
	return p.Ident != "B" && p.Ident != "W"
}

func movesFromGame(g *sgf.GameTree, boardSize int) ([]*move, error) {
	ret := []*move{}
	for _, n := range g.Nodes {
		for _, p := range n.Properties {
			if notAMove(p) {
				continue
			}
			if len(p.Values) != 1 {
				return nil, fmt.Errorf("malformed move: %#v", p.Values)
			}

			x, y, pass, err := parseMoveValue(p.Values[0], boardSize)
			if err != nil {
				return nil, err
			}

			m := &move{
				white: p.Ident == "W",
				pass:  pass,
				x:     x,
				y:     y,
			}
			ret = append(ret, m)
		}
	}

	switch n := len(g.GameTrees); n {
	case 0:
		return ret, nil
	case 1:
	default:
		log.Printf("found %d variations: using the first, ignoring the rest", n)
	}

	childMoves, err := movesFromGame(g.GameTrees[0], boardSize)
	if err != nil {
		return nil, err
	}

	return append(ret, childMoves...), nil
}

func actionsFromGame(g *sgf.GameTree, boardSize int, variationPath []int) ([]*action, error) {
	actions, branchDepth, err := actionsFromGameAtDepth(g, boardSize, variationPath, 0)
	if err != nil {
		return nil, err
	}
	if branchDepth != len(variationPath) {
		return nil, fmt.Errorf("variation-path has too many entries: %v", variationPath)
	}
	return actions, nil
}

func actionsFromGameAtDepth(g *sgf.GameTree, boardSize int, variationPath []int, branchDepth int) ([]*action, int, error) {
	ret := []*action{}
	for _, n := range g.Nodes {
		a, hasEffect, err := actionFromNode(n, boardSize)
		if err != nil {
			return nil, branchDepth, err
		}
		if hasEffect {
			ret = append(ret, a)
		}
	}

	switch n := len(g.GameTrees); n {
	case 0:
		return ret, branchDepth, nil
	case 1:
		childActions, consumed, err := actionsFromGameAtDepth(g.GameTrees[0], boardSize, variationPath, branchDepth)
		if err != nil {
			return nil, consumed, err
		}
		return append(ret, childActions...), consumed, nil
	}

	selected := 0
	if branchDepth < len(variationPath) {
		selected = variationPath[branchDepth] - 1
	}
	if selected < 0 || selected >= len(g.GameTrees) {
		return nil, branchDepth, fmt.Errorf("variation index %d out of range at branch %d (choices: %d)", selected+1, branchDepth+1, len(g.GameTrees))
	}

	log.Printf("found %d variations at branch %d: using #%d", len(g.GameTrees), branchDepth+1, selected+1)

	childActions, consumed, err := actionsFromGameAtDepth(g.GameTrees[selected], boardSize, variationPath, branchDepth+1)
	if err != nil {
		return nil, consumed, err
	}

	return append(ret, childActions...), consumed, nil
}

func actionFromNode(node *sgf.Node, boardSize int) (*action, bool, error) {
	a := &action{}

	edits, err := parseBoardEdits(node, "AB", black, boardSize)
	if err != nil {
		return nil, false, err
	}
	a.setups = append(a.setups, edits...)

	edits, err = parseBoardEdits(node, "AW", white, boardSize)
	if err != nil {
		return nil, false, err
	}
	a.setups = append(a.setups, edits...)

	edits, err = parseBoardEdits(node, "AE", background, boardSize)
	if err != nil {
		return nil, false, err
	}
	a.setups = append(a.setups, edits...)

	marks, err := parseBoardMarks(node, "TR", markTriangle, boardSize)
	if err != nil {
		return nil, false, err
	}
	a.marks = append(a.marks, marks...)

	marks, err = parseBoardMarks(node, "SQ", markSquare, boardSize)
	if err != nil {
		return nil, false, err
	}
	a.marks = append(a.marks, marks...)

	marks, err = parseBoardMarks(node, "CR", markCircle, boardSize)
	if err != nil {
		return nil, false, err
	}
	a.marks = append(a.marks, marks...)

	marks, err = parseBoardMarks(node, "MA", markCross, boardSize)
	if err != nil {
		return nil, false, err
	}
	a.marks = append(a.marks, marks...)

	marks, err = parseBoardMarks(node, "SL", markSelected, boardSize)
	if err != nil {
		return nil, false, err
	}
	a.marks = append(a.marks, marks...)

	marks, err = parseBoardMarks(node, "TB", markTerritoryBlack, boardSize)
	if err != nil {
		return nil, false, err
	}
	a.marks = append(a.marks, marks...)

	marks, err = parseBoardMarks(node, "TW", markTerritoryWhite, boardSize)
	if err != nil {
		return nil, false, err
	}
	a.marks = append(a.marks, marks...)

	labels, err := parseBoardLabels(node, boardSize)
	if err != nil {
		return nil, false, err
	}
	a.labels = append(a.labels, labels...)

	moveCount := 0
	for _, p := range node.Properties {
		if notAMove(p) {
			continue
		}
		moveCount++
		if moveCount > 1 {
			return nil, false, fmt.Errorf("node has more than one move")
		}
		if len(p.Values) != 1 {
			return nil, false, fmt.Errorf("malformed move: %#v", p.Values)
		}

		x, y, pass, err := parseMoveValue(p.Values[0], boardSize)
		if err != nil {
			return nil, false, err
		}

		a.move = &move{
			white: p.Ident == "W",
			pass:  pass,
			x:     x,
			y:     y,
		}
	}

	if value := propertyValue(node, "MN"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return nil, false, fmt.Errorf("malformed move number: %s", value)
		}
		a.moveNumber = n
	}

	if value := propertyValue(node, "PL"); value != "" {
		player, err := parsePlayerColor(value)
		if err != nil {
			return nil, false, err
		}
		a.toPlay = player
	}
	a.comment = normalizeCommentText(propertyValue(node, "C"))

	return a, a.move != nil || len(a.setups) > 0 || a.toPlay != background || len(a.marks) > 0 || len(a.labels) > 0 || a.comment != "", nil
}

func boardSizeFromGame(g *sgf.GameTree) (int, error) {
	root, err := rootNode(g)
	if err != nil {
		return 0, err
	}

	for _, p := range root.Properties {
		if p.Ident != "SZ" {
			continue
		}
		if len(p.Values) != 1 {
			return 0, fmt.Errorf("malformed board size: %#v", p.Values)
		}
		return parseBoardSize(p.Values[0])
	}

	return defaultBoardSize, nil
}

func gameInfoFromGame(g *sgf.GameTree) (*gameInfo, error) {
	root, err := rootNode(g)
	if err != nil {
		return nil, err
	}

	return &gameInfo{
		gameName:  propertyValue(root, "GN"),
		date:      propertyValue(root, "DT"),
		result:    propertyValue(root, "RE"),
		rules:     propertyValue(root, "RU"),
		komi:      propertyValue(root, "KM"),
		handicap:  propertyValue(root, "HA"),
		blackName: propertyValue(root, "PB"),
		blackRank: propertyValue(root, "BR"),
		whiteName: propertyValue(root, "PW"),
		whiteRank: propertyValue(root, "WR"),
	}, nil
}

func initialBoardFromGame(boardSize int) (*boardState, error) {
	return newBoardState(boardSize), nil
}

func koRuleFromGame(g *sgf.GameTree) (koRule, error) {
	root, err := rootNode(g)
	if err != nil {
		return simpleKoRule, err
	}

	rules := strings.ToLower(propertyValue(root, "RU"))
	switch {
	case strings.Contains(rules, "japanese"), strings.Contains(rules, "korean"):
		return simpleKoRule, nil
	default:
		return positionalSuperkoRule, nil
	}
}

func parseBoardEdits(node *sgf.Node, ident string, stone uint8, boardSize int) ([]boardEdit, error) {
	ret := []boardEdit{}
	for _, value := range propertyValues(node, ident) {
		points, err := expandPointSpec(value, boardSize)
		if err != nil {
			return nil, err
		}
		for _, p := range points {
			ret = append(ret, boardEdit{x: p.x, y: p.y, stone: stone})
		}
	}
	return ret, nil
}

func parseBoardMarks(node *sgf.Node, ident string, kind markKind, boardSize int) ([]boardMark, error) {
	ret := []boardMark{}
	for _, value := range propertyValues(node, ident) {
		points, err := expandPointSpec(value, boardSize)
		if err != nil {
			return nil, err
		}
		for _, p := range points {
			ret = append(ret, boardMark{x: p.x, y: p.y, kind: kind})
		}
	}
	return ret, nil
}

func parseBoardLabels(node *sgf.Node, boardSize int) ([]boardLabel, error) {
	ret := []boardLabel{}
	for _, value := range propertyValues(node, "LB") {
		parts := strings.SplitN(value, ":", 2)
		if len(parts) != 2 || parts[1] == "" {
			return nil, fmt.Errorf("malformed label specification: %s", value)
		}
		x, y, err := parsePointValue(parts[0], boardSize)
		if err != nil {
			return nil, err
		}
		ret = append(ret, boardLabel{x: x, y: y, text: normalizeInlineText(parts[1])})
	}
	return ret, nil
}

func parsePlayerColor(value string) (uint8, error) {
	switch value {
	case "B":
		return black, nil
	case "W":
		return white, nil
	default:
		return background, fmt.Errorf("malformed player color: %s", value)
	}
}

func normalizeInlineText(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\n", " ")), " ")
}

func normalizeCommentText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	lines := strings.Split(value, "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		normalized = append(normalized, strings.Join(strings.Fields(line), " "))
	}
	return strings.TrimSpace(strings.Join(normalized, "\n"))
}

func expandPointSpec(value string, boardSize int) ([]point, error) {
	if value == "" {
		return nil, fmt.Errorf("empty point specification")
	}
	if !strings.Contains(value, ":") {
		x, y, err := parsePointValue(value, boardSize)
		if err != nil {
			return nil, err
		}
		return []point{{x: x, y: y}}, nil
	}

	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("malformed point specification: %s", value)
	}

	x1, y1, err := parsePointValue(parts[0], boardSize)
	if err != nil {
		return nil, err
	}
	x2, y2, err := parsePointValue(parts[1], boardSize)
	if err != nil {
		return nil, err
	}

	minX, maxX := x1, x2
	if minX > maxX {
		minX, maxX = maxX, minX
	}
	minY, maxY := y1, y2
	if minY > maxY {
		minY, maxY = maxY, minY
	}

	points := make([]point, 0, (maxX-minX+1)*(maxY-minY+1))
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			points = append(points, point{x: x, y: y})
		}
	}
	return points, nil
}

func parsePointValue(value string, boardSize int) (int, int, error) {
	if len(value) != 2 {
		return 0, 0, fmt.Errorf("malformed point value: %s", value)
	}

	x := int(value[0] - 'a')
	y := int(value[1] - 'a')
	if x < 0 || y < 0 || x >= boardSize || y >= boardSize {
		return 0, 0, fmt.Errorf("point out of range for %dx%d board: %s", boardSize, boardSize, value)
	}

	return x, y, nil
}

func parseVariationPath(value string) ([]int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}

	parts := strings.Split(value, ",")
	path := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("malformed variation-path: %s", value)
		}
		n, err := strconv.Atoi(part)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("variation-path must use positive integers: %s", value)
		}
		path = append(path, n)
	}

	return path, nil
}

func variationLabel(path []int) string {
	if len(path) == 0 {
		return "Main line"
	}

	parts := make([]string, 0, len(path))
	for _, p := range path {
		parts = append(parts, strconv.Itoa(p))
	}
	return "Variation " + strings.Join(parts, ".")
}

func collectVariationPaths(g *sgf.GameTree) [][]int {
	paths := collectVariationPathsAt(g, nil)
	if len(paths) == 0 {
		return [][]int{nil}
	}
	return paths
}

func collectVariationPathsAt(g *sgf.GameTree, prefix []int) [][]int {
	switch len(g.GameTrees) {
	case 0:
		return [][]int{append([]int(nil), prefix...)}
	case 1:
		return collectVariationPathsAt(g.GameTrees[0], prefix)
	default:
		ret := [][]int{}
		for i, child := range g.GameTrees {
			next := append(append([]int(nil), prefix...), i+1)
			ret = append(ret, collectVariationPathsAt(child, next)...)
		}
		return ret
	}
}

func variationOutputPath(base string, path []int, total int) string {
	if total <= 1 {
		return base
	}

	label := "main"
	if len(path) > 0 {
		parts := make([]string, 0, len(path))
		for _, p := range path {
			parts = append(parts, strconv.Itoa(p))
		}
		label = strings.Join(parts, "-")
	}

	ext := ""
	name := base
	if dot := strings.LastIndex(base, "."); dot > 0 {
		name = base[:dot]
		ext = base[dot:]
	}
	return fmt.Sprintf("%s.var-%s%s", name, label, ext)
}

func katagoVisitsForStrength(value string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "fast":
		return 100, nil
	case "strong":
		return 1000, nil
	case "monster":
		return 10000, nil
	case "":
		return 0, nil
	default:
		return 0, fmt.Errorf("katago-strength must be one of: fast, strong, monster")
	}
}

func normalizeKataGoView(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "black":
		return "black", nil
	case "white":
		return "white", nil
	default:
		return "", fmt.Errorf("katago-view must be one of: black, white")
	}
}

func parseBoardSize(value string) (int, error) {
	if strings.Contains(value, ":") {
		parts := strings.SplitN(value, ":", 2)
		if parts[0] != parts[1] {
			return 0, fmt.Errorf("rectangular boards are not supported: %s", value)
		}
		value = parts[0]
	}

	size, err := strconv.Atoi(value)
	if err != nil || size <= 0 {
		return 0, fmt.Errorf("malformed board size: %s", value)
	}

	return size, nil
}

func parseMoveValue(value string, boardSize int) (int, int, bool, error) {
	if value == "" {
		return 0, 0, true, nil
	}

	if len(value) != 2 {
		return 0, 0, false, fmt.Errorf("malformed move value: %s", value)
	}

	x := int(value[0] - 'a')
	y := int(value[1] - 'a')
	if x < 0 || y < 0 || x >= boardSize || y >= boardSize {
		return 0, 0, false, fmt.Errorf("move out of range for %dx%d board: %s", boardSize, boardSize, value)
	}

	return x, y, false, nil
}

func actionsToFrames(info *gameInfo, initial *boardState, actions []*action, cfg renderConfig, rule koRule) ([]*image.Paletted, error) {
	specs, err := actionsToFrameSpecs(initial, actions, rule)
	if err != nil {
		return nil, err
	}

	ret := make([]*image.Paletted, 0, len(specs))
	for i, spec := range specs {
		frameCfg := cfg
		frameCfg.currentFrame = i
		ret = append(ret, renderFrame(info, spec.state, spec.current, spec.moveNumber, spec.capturedThisMove, frameCfg))
	}
	return ret, nil
}

type frameSpec struct {
	state            *boardState
	beforeMoveState  *boardState
	current          *action
	moveNumber       int
	capturedThisMove int
}

func actionsToFrameSpecs(initial *boardState, actions []*action, rule koRule) ([]frameSpec, error) {
	ret := []frameSpec{}
	state := initial.clone()
	history := []string{state.hash()}
	moveNumber := 0

	if len(actions) == 0 {
		return append(ret, frameSpec{state: state.clone()}), nil
	}

	for _, a := range actions {
		if len(a.setups) > 0 {
			for _, edit := range a.setups {
				state.applyBoardEdit(edit)
			}
			history = []string{state.hash()}
		}
		if a.toPlay != background {
			state.toPlay = a.toPlay
		}

		if a.move == nil {
			if len(a.setups) > 0 || a.toPlay != background || len(a.marks) > 0 || len(a.labels) > 0 || a.comment != "" {
				ret = append(ret, frameSpec{
					state:            state.clone(),
					current:          a,
					moveNumber:       moveNumber,
					capturedThisMove: 0,
				})
			}
			continue
		}

		currentMoveNumber := moveNumber + 1
		if a.moveNumber > 0 {
			currentMoveNumber = a.moveNumber
		}
		beforeMoveState := state.clone()

		captured, err := state.applyMove(a.move, history, currentMoveNumber, rule)
		if err != nil {
			return nil, err
		}

		moveNumber = currentMoveNumber
		history = append(history, state.hash())
		ret = append(ret, frameSpec{
			state:            state.clone(),
			beforeMoveState:  beforeMoveState,
			current:          a,
			moveNumber:       moveNumber,
			capturedThisMove: captured,
		})
	}
	return ret, nil
}

func framesToGif(frames []*image.Paletted) (*gif.GIF, error) {
	g := &gif.GIF{LoopCount: len(frames)}
	for _, f := range frames {
		g.Image = append(g.Image, f)
		g.Delay = append(g.Delay, delay)
	}
	return g, nil
}

var palette = []color.Color{
	color.RGBA{0xE6, 0xBF, 0x83, 0xFF}, // wood
	color.Black,
	color.White,
	color.RGBA{0x63, 0x3D, 0x14, 0xFF}, // grid
	color.RGBA{0xD1, 0x2B, 0x2B, 0xFF}, // highlight
	color.RGBA{0x2A, 0x66, 0xC9, 0xFF}, // analysis blue
	color.RGBA{0x1D, 0x8F, 0x5A, 0xFF}, // analysis green
	color.RGBA{0x8B, 0x8B, 0x8B, 0xFF}, // analysis gray
	color.RGBA{0xD9, 0x7A, 0x0B, 0xFF}, // analysis orange
}

const (
	background = iota
	black
	white
	gridLine
	highlight
	analysisBlue
	analysisGreen
	analysisGray
	analysisOrange
)

const (
	delay             = 100 // delay between frames in 10ms units
	stoneDiameter     = 40  // pixels
	defaultBoardSize  = 19
	coordMargin       = 40
	infoHeight        = 92
	compactInfoHeight = 64
	analysisHeight    = 148
	textPadding       = 8
)

var fullRenderLayout = renderLayout{infoHeight: infoHeight}
var compactRenderLayout = renderLayout{infoHeight: compactInfoHeight}

// side of the board in pixels
func side(boardSize int) int {
	return boardSize*stoneDiameter + 2
}

type boardState struct {
	size          int
	points        []uint8
	moveNumbers   []int
	toPlay        uint8
	capturesBlack int
	capturesWhite int
}

type point struct {
	x int
	y int
}

func newBoardState(size int) *boardState {
	return &boardState{
		size:        size,
		points:      make([]uint8, size*size),
		moveNumbers: make([]int, size*size),
		toPlay:      black,
	}
}

func (b *boardState) clone() *boardState {
	points := make([]uint8, len(b.points))
	copy(points, b.points)
	moveNumbers := make([]int, len(b.moveNumbers))
	copy(moveNumbers, b.moveNumbers)
	return &boardState{
		size:          b.size,
		points:        points,
		moveNumbers:   moveNumbers,
		toPlay:        b.toPlay,
		capturesBlack: b.capturesBlack,
		capturesWhite: b.capturesWhite,
	}
}

func (b *boardState) index(x, y int) int {
	return y*b.size + x
}

func (b *boardState) inBounds(x, y int) bool {
	return x >= 0 && y >= 0 && x < b.size && y < b.size
}

func (b *boardState) get(x, y int) uint8 {
	return b.points[b.index(x, y)]
}

func (b *boardState) set(x, y int, stone uint8) {
	b.points[b.index(x, y)] = stone
}

func (b *boardState) setMoveNumber(x, y, moveNumber int) {
	b.moveNumbers[b.index(x, y)] = moveNumber
}

func (b *boardState) moveNumberAt(x, y int) int {
	return b.moveNumbers[b.index(x, y)]
}

func (b *boardState) hash() string {
	return string(b.points)
}

func (b *boardState) applyBoardEdit(edit boardEdit) {
	b.set(edit.x, edit.y, edit.stone)
	if edit.stone == background {
		b.setMoveNumber(edit.x, edit.y, 0)
		return
	}
	b.setMoveNumber(edit.x, edit.y, 0)
}

func (b *boardState) applyMove(m *move, history []string, moveNumber int, rule koRule) (int, error) {
	next := b.clone()
	stone := uint8(black)
	opponent := uint8(white)
	if m.white {
		stone, opponent = white, black
	}
	if m.pass {
		next.toPlay = opponent
		*b = *next
		return 0, nil
	}

	if !next.inBounds(m.x, m.y) {
		return 0, fmt.Errorf("move out of range: (%d, %d)", m.x, m.y)
	}
	if next.get(m.x, m.y) != background {
		return 0, fmt.Errorf("intersection already occupied: (%d, %d)", m.x, m.y)
	}

	next.set(m.x, m.y, stone)
	next.setMoveNumber(m.x, m.y, moveNumber)
	captured := 0
	for _, n := range neighbors(m.x, m.y) {
		if !next.inBounds(n.x, n.y) || next.get(n.x, n.y) != opponent {
			continue
		}
		group, liberties := next.groupAt(n.x, n.y)
		if liberties == 0 {
			for _, p := range group {
				next.set(p.x, p.y, background)
				next.setMoveNumber(p.x, p.y, 0)
			}
			captured += len(group)
		}
	}

	_, liberties := next.groupAt(m.x, m.y)
	if liberties == 0 {
		return 0, fmt.Errorf("suicide move at (%d, %d)", m.x, m.y)
	}

	if violatesKo(rule, next.hash(), history) {
		return 0, fmt.Errorf("ko violation at (%d, %d)", m.x, m.y)
	}

	if stone == black {
		next.capturesBlack += captured
	} else {
		next.capturesWhite += captured
	}
	next.toPlay = opponent

	*b = *next
	return captured, nil
}

func (b *boardState) opponentOf(stone uint8) uint8 {
	if stone == white {
		return black
	}
	return white
}

func violatesKo(rule koRule, nextHash string, history []string) bool {
	switch rule {
	case positionalSuperkoRule:
		for _, h := range history {
			if nextHash == h {
				return true
			}
		}
		return false
	default:
		return len(history) >= 2 && nextHash == history[len(history)-2]
	}
}

func (b *boardState) groupAt(x, y int) ([]point, int) {
	color := b.get(x, y)
	if color == background {
		return nil, 0
	}

	group := []point{}
	queue := []point{{x: x, y: y}}
	seen := map[int]bool{b.index(x, y): true}
	liberties := map[int]bool{}

	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		group = append(group, p)

		for _, n := range neighbors(p.x, p.y) {
			if !b.inBounds(n.x, n.y) {
				continue
			}
			switch b.get(n.x, n.y) {
			case background:
				liberties[b.index(n.x, n.y)] = true
			case color:
				idx := b.index(n.x, n.y)
				if !seen[idx] {
					seen[idx] = true
					queue = append(queue, n)
				}
			}
		}
	}

	return group, len(liberties)
}

func renderFrame(info *gameInfo, state *boardState, current *action, moveNumber, capturedThisMove int, cfg renderConfig) *image.Paletted {
	if current == nil {
		current = &action{}
	}

	layout := cfg.layout.normalized()
	boardSide := side(state.size)
	width := boardSide + 2*coordMargin
	height := layout.infoHeight + boardSide + 2*coordMargin + layout.analysisHeight
	rect := image.Rect(0, 0, width, height)
	img := image.NewPaletted(rect, palette)
	fill(img, background)

	drawInfo(img, info, state, current, moveNumber, capturedThisMove, cfg)
	drawCoordinatesWithLayout(img, state.size, layout)
	drawBoardWithLayout(img, state, layout)
	drawStarPointsWithLayout(img, state.size, layout)
	drawStonesWithLayout(img, state, layout)
	if cfg.showMoveNumbers || cfg.recentMoves > 0 {
		drawStoneMoveNumbersWithLayout(img, state, moveNumber, cfg.showMoveNumbers, cfg.recentMoves, layout)
	}
	drawBoardAnnotationsWithLayout(img, state, current, layout)
	drawAnalysisRecommendations(img, state, cfg)
	drawLastMoveMarkerWithLayout(img, current.move, layout)
	drawAnalysisPanel(img, cfg)

	return img
}

func selectRenderLayout(actions []*action, hasAnalysis bool) renderLayout {
	layout := compactRenderLayout
	for _, action := range actions {
		if summarizeComment(action.comment, 120) != "" {
			layout = fullRenderLayout
			break
		}
	}
	if hasAnalysis {
		layout.analysisHeight = analysisHeight
	}
	return layout
}

func (l renderLayout) normalized() renderLayout {
	if l.infoHeight <= 0 {
		l.infoHeight = fullRenderLayout.infoHeight
	}
	if l.analysisHeight < 0 {
		l.analysisHeight = 0
	}
	return l
}

func drawInfo(img *image.Paletted, info *gameInfo, state *boardState, current *action, moveNumber, capturedThisMove int, cfg renderConfig) {
	lastMove := current.move
	blackPlayer := formatPlayer(info.blackName, info.blackRank)
	whitePlayer := formatPlayer(info.whiteName, info.whiteRank)
	line2Parts := []string{}
	line3Parts := []string{}
	if info.gameName != "" {
		line2Parts = append(line2Parts, info.gameName)
	}
	if info.date != "" {
		line2Parts = append(line2Parts, info.date)
	}
	if info.result != "" {
		line2Parts = append(line2Parts, info.result)
	}
	if info.rules != "" {
		line2Parts = append(line2Parts, "Rules "+info.rules)
	}
	if info.komi != "" {
		line2Parts = append(line2Parts, "Komi "+info.komi)
	}
	if info.handicap != "" {
		line2Parts = append(line2Parts, "HA "+info.handicap)
	}
	if cfg.variationLabel != "" {
		line2Parts = append(line2Parts, cfg.variationLabel)
	}
	line3Parts = append(line3Parts, moveLabel(lastMove, moveNumber, state.size))
	line3Parts = append(line3Parts, fmt.Sprintf("Capture +%d", capturedThisMove))
	line3Parts = append(line3Parts, fmt.Sprintf("Total Caps B:%d W:%d", state.capturesBlack, state.capturesWhite))
	line3Parts = append(line3Parts, "To play: "+playerLabel(state.toPlay))
	comment := summarizeComment(current.comment, 120)

	drawPlayerHeader(img, 18, blackPlayer, whitePlayer)
	drawText(img, textPadding, 36, strings.Join(line2Parts, " | "), color.Black)
	drawText(img, textPadding, 52, strings.Join(line3Parts, " | "), color.Black)
	if comment != "" {
		drawWrappedText(img, textPadding, 68, img.Bounds().Dx()-2*textPadding, 2, "Comment: "+comment, color.Black)
	}
}

func drawPlayerHeader(img *image.Paletted, baselineY int, blackPlayer, whitePlayer string) {
	const (
		stoneRadius = 8
		stoneGap    = 8
		playerGap   = 20
		textYOffset = 7
	)

	headerFace := playerHeaderFace
	blackWidth := stoneRadius*2 + stoneGap + measureTextWidthWithFace(blackPlayer, headerFace)
	whiteWidth := stoneRadius*2 + stoneGap + measureTextWidthWithFace(whitePlayer, headerFace)
	totalWidth := blackWidth + playerGap + whiteWidth
	startX := img.Bounds().Dx()/2 - totalWidth/2
	centerY := baselineY - textYOffset

	drawInfoStone(img, startX+stoneRadius, centerY, stoneRadius, black)
	drawTextWithFace(img, startX+stoneRadius*2+stoneGap, baselineY, blackPlayer, color.Black, headerFace)

	rightX := startX + blackWidth + playerGap
	drawInfoStone(img, rightX+stoneRadius, centerY, stoneRadius, white)
	drawTextWithFace(img, rightX+stoneRadius*2+stoneGap, baselineY, whitePlayer, color.Black, headerFace)
}

func drawInfoStone(img *image.Paletted, centerX, centerY, radius int, stone uint8) {
	if stone == white {
		for x := centerX - radius; x <= centerX+radius; x++ {
			for y := centerY - radius; y <= centerY+radius; y++ {
				d := dist(x, y, centerX, centerY)
				if d <= radius {
					img.SetColorIndex(x, y, black)
				}
			}
		}
		for x := centerX - (radius - 1); x <= centerX+(radius-1); x++ {
			for y := centerY - (radius - 1); y <= centerY+(radius-1); y++ {
				if dist(x, y, centerX, centerY) <= radius-1 {
					img.SetColorIndex(x, y, white)
				}
			}
		}
		return
	}

	for x := centerX - radius; x <= centerX+radius; x++ {
		for y := centerY - radius; y <= centerY+radius; y++ {
			if dist(x, y, centerX, centerY) <= radius {
				img.SetColorIndex(x, y, stone)
			}
		}
	}
}

func drawCoordinates(img *image.Paletted, boardSize int) {
	drawCoordinatesWithLayout(img, boardSize, fullRenderLayout)
}

func drawCoordinatesWithLayout(img *image.Paletted, boardSize int, layout renderLayout) {
	topY := boardOriginYForLayout(layout) - stoneDiameter/2 - 6
	bottomY := boardOriginYForLayout(layout) + (boardSize-1)*stoneDiameter + stoneDiameter/2 + 14
	leftX := boardOriginX() - stoneDiameter/2 - 12
	rightX := boardOriginX() + (boardSize-1)*stoneDiameter + stoneDiameter/2 + 12

	for i := 0; i < boardSize; i++ {
		x := boardOriginX() + i*stoneDiameter
		label := columnLabel(i)
		drawCenteredText(img, x, topY, label, color.Black)
		drawCenteredText(img, x, bottomY, label, color.Black)
	}

	for i := 0; i < boardSize; i++ {
		y := boardOriginYForLayout(layout) + i*stoneDiameter + 5
		label := strconv.Itoa(boardSize - i)
		drawCenteredText(img, leftX, y, label, color.Black)
		drawCenteredText(img, rightX, y, label, color.Black)
	}
}

func drawBoard(img *image.Paletted, state *boardState) {
	drawBoardWithLayout(img, state, fullRenderLayout)
}

func drawBoardWithLayout(img *image.Paletted, state *boardState, layout renderLayout) {
	left := boardOriginX()
	top := boardOriginYForLayout(layout)
	span := (state.size - 1) * stoneDiameter

	for i := 0; i < state.size; i++ {
		x := left + i*stoneDiameter
		for y := top; y <= top+span; y++ {
			img.SetColorIndex(x, y, gridLine)
		}
	}

	for i := 0; i < state.size; i++ {
		y := top + i*stoneDiameter
		for x := left; x <= left+span; x++ {
			img.SetColorIndex(x, y, gridLine)
		}
	}
}

func drawStarPoints(img *image.Paletted, boardSize int) {
	drawStarPointsWithLayout(img, boardSize, fullRenderLayout)
}

func drawStarPointsWithLayout(img *image.Paletted, boardSize int, layout renderLayout) {
	for _, p := range starPoints(boardSize) {
		centerX := boardOriginX() + p.x*stoneDiameter
		centerY := boardOriginYForLayout(layout) + p.y*stoneDiameter
		for x := centerX - 3; x <= centerX+3; x++ {
			for y := centerY - 3; y <= centerY+3; y++ {
				if dist(x, y, centerX, centerY) <= 3 {
					img.SetColorIndex(x, y, black)
				}
			}
		}
	}
}

func drawStones(img *image.Paletted, state *boardState) {
	drawStonesWithLayout(img, state, fullRenderLayout)
}

func drawStonesWithLayout(img *image.Paletted, state *boardState, layout renderLayout) {
	for y := 0; y < state.size; y++ {
		for x := 0; x < state.size; x++ {
			stone := state.get(x, y)
			if stone == background {
				continue
			}
			drawStoneWithLayout(img, x, y, stone, layout)
		}
	}
}

func drawStoneMoveNumbers(img *image.Paletted, state *boardState, currentMove int, showAll bool, recentMoves int) {
	drawStoneMoveNumbersWithLayout(img, state, currentMove, showAll, recentMoves, fullRenderLayout)
}

func drawStoneMoveNumbersWithLayout(img *image.Paletted, state *boardState, currentMove int, showAll bool, recentMoves int, layout renderLayout) {
	minMove := 1
	if !showAll && recentMoves > 0 {
		minMove = currentMove - recentMoves + 1
		if minMove < 1 {
			minMove = 1
		}
	}

	for y := 0; y < state.size; y++ {
		for x := 0; x < state.size; x++ {
			stone := state.get(x, y)
			if stone == background {
				continue
			}

			moveNumber := state.moveNumberAt(x, y)
			if moveNumber == 0 {
				continue
			}
			if !showAll && recentMoves > 0 && moveNumber < minMove {
				continue
			}

			textColor := color.White
			if stone == white {
				textColor = color.Black
			}
			centerX := boardOriginX() + x*stoneDiameter
			centerY := boardOriginYForLayout(layout) + y*stoneDiameter + 5
			drawCenteredText(img, centerX, centerY, strconv.Itoa(moveNumber), textColor)
		}
	}
}

func drawBoardAnnotations(img *image.Paletted, state *boardState, current *action) {
	drawBoardAnnotationsWithLayout(img, state, current, fullRenderLayout)
}

func drawBoardAnnotationsWithLayout(img *image.Paletted, state *boardState, current *action, layout renderLayout) {
	if current == nil {
		return
	}

	for _, mark := range current.marks {
		drawMarkWithLayout(img, state, mark, layout)
	}
	for _, label := range current.labels {
		drawLabelWithLayout(img, state, label, layout)
	}
}

func drawMark(img *image.Paletted, state *boardState, mark boardMark) {
	drawMarkWithLayout(img, state, mark, fullRenderLayout)
}

func drawMarkWithLayout(img *image.Paletted, state *boardState, mark boardMark, layout renderLayout) {
	centerX := boardOriginX() + mark.x*stoneDiameter
	centerY := boardOriginYForLayout(layout) + mark.y*stoneDiameter
	colorIndex := annotationColor(state, mark.x, mark.y)

	switch mark.kind {
	case markCircle:
		drawCircleOutline(img, centerX, centerY, stoneDiameter/4, colorIndex)
	case markSquare:
		drawSquareOutline(img, centerX, centerY, stoneDiameter/4, colorIndex)
	case markTriangle:
		drawTriangleOutline(img, centerX, centerY, stoneDiameter/4, colorIndex)
	case markCross:
		drawCrossMark(img, centerX, centerY, stoneDiameter/4, colorIndex)
	case markSelected:
		drawCircleOutline(img, centerX, centerY, stoneDiameter/3, highlight)
	case markTerritoryBlack:
		drawTerritoryDot(img, centerX, centerY, black)
	case markTerritoryWhite:
		drawTerritoryDot(img, centerX, centerY, white)
	}
}

func drawLabel(img *image.Paletted, state *boardState, label boardLabel) {
	drawLabelWithLayout(img, state, label, fullRenderLayout)
}

func drawLabelWithLayout(img *image.Paletted, state *boardState, label boardLabel, layout renderLayout) {
	if label.text == "" {
		return
	}
	centerX := boardOriginX() + label.x*stoneDiameter
	centerY := boardOriginYForLayout(layout) + label.y*stoneDiameter + 5
	drawCenteredText(img, centerX, centerY, label.text, annotationTextColor(state, label.x, label.y))
}

func drawLastMoveMarker(img *image.Paletted, lastMove *move) {
	drawLastMoveMarkerWithLayout(img, lastMove, fullRenderLayout)
}

func drawLastMoveMarkerWithLayout(img *image.Paletted, lastMove *move, layout renderLayout) {
	if lastMove == nil || lastMove.pass {
		return
	}

	x := boardOriginX() + lastMove.x*stoneDiameter
	y := boardOriginYForLayout(layout) + lastMove.y*stoneDiameter
	outerRadius := stoneDiameter/2 - 3
	innerRadius := outerRadius - 3
	for px := x - outerRadius; px <= x+outerRadius; px++ {
		for py := y - outerRadius; py <= y+outerRadius; py++ {
			d := dist(px, py, x, y)
			if d <= outerRadius && d >= innerRadius {
				img.SetColorIndex(px, py, highlight)
			}
		}
	}
}

func drawCircleOutline(img *image.Paletted, centerX, centerY, radius int, colorIndex uint8) {
	inner := radius - 2
	for x := centerX - radius; x <= centerX+radius; x++ {
		for y := centerY - radius; y <= centerY+radius; y++ {
			d := dist(x, y, centerX, centerY)
			if d <= radius && d >= inner {
				img.SetColorIndex(x, y, colorIndex)
			}
		}
	}
}

func drawSquareOutline(img *image.Paletted, centerX, centerY, halfSize int, colorIndex uint8) {
	for x := centerX - halfSize; x <= centerX+halfSize; x++ {
		img.SetColorIndex(x, centerY-halfSize, colorIndex)
		img.SetColorIndex(x, centerY+halfSize, colorIndex)
	}
	for y := centerY - halfSize; y <= centerY+halfSize; y++ {
		img.SetColorIndex(centerX-halfSize, y, colorIndex)
		img.SetColorIndex(centerX+halfSize, y, colorIndex)
	}
}

func drawTriangleOutline(img *image.Paletted, centerX, centerY, radius int, colorIndex uint8) {
	top := point{x: centerX, y: centerY - radius}
	left := point{x: centerX - radius, y: centerY + radius/2}
	right := point{x: centerX + radius, y: centerY + radius/2}
	drawLine(img, top.x, top.y, left.x, left.y, colorIndex)
	drawLine(img, left.x, left.y, right.x, right.y, colorIndex)
	drawLine(img, right.x, right.y, top.x, top.y, colorIndex)
}

func drawCrossMark(img *image.Paletted, centerX, centerY, radius int, colorIndex uint8) {
	drawLine(img, centerX-radius, centerY-radius, centerX+radius, centerY+radius, colorIndex)
	drawLine(img, centerX-radius, centerY+radius, centerX+radius, centerY-radius, colorIndex)
}

func drawTerritoryDot(img *image.Paletted, centerX, centerY int, colorIndex uint8) {
	for x := centerX - 4; x <= centerX+4; x++ {
		for y := centerY - 4; y <= centerY+4; y++ {
			if dist(x, y, centerX, centerY) <= 4 {
				img.SetColorIndex(x, y, colorIndex)
			}
		}
	}
}

func drawLine(img *image.Paletted, x0, y0, x1, y1 int, colorIndex uint8) {
	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	sx := -1
	if x0 < x1 {
		sx = 1
	}
	sy := -1
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy

	for {
		img.SetColorIndex(x0, y0, colorIndex)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func drawStone(img *image.Paletted, gridX, gridY int, stone uint8) {
	drawStoneWithLayout(img, gridX, gridY, stone, fullRenderLayout)
}

func drawStoneWithLayout(img *image.Paletted, gridX, gridY int, stone uint8, layout renderLayout) {
	centerX := boardOriginX() + gridX*stoneDiameter
	centerY := boardOriginYForLayout(layout) + gridY*stoneDiameter
	radius := stoneDiameter / 2
	for x := centerX - radius; x <= centerX+radius; x++ {
		for y := centerY - radius; y <= centerY+radius; y++ {
			if dist(x, y, centerX, centerY) <= radius {
				img.SetColorIndex(x, y, stone)
			}
		}
	}
}

func fill(img *image.Paletted, colorIndex uint8) {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			img.SetColorIndex(x, y, colorIndex)
		}
	}
}

func drawText(img *image.Paletted, x, baselineY int, text string, textColor color.Color) {
	drawTextWithFace(img, x, baselineY, text, textColor, basicfont.Face7x13)
}

func drawTextWithFace(img *image.Paletted, x, baselineY int, text string, textColor color.Color, face font.Face) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(textColor),
		Face: face,
		Dot:  fixed.P(x, baselineY),
	}
	d.DrawString(text)
}

func drawCenteredText(img *image.Paletted, centerX, baselineY int, text string, textColor color.Color) {
	width := measureTextWidth(text)
	drawText(img, centerX-width/2, baselineY, text, textColor)
}

func drawWrappedText(img *image.Paletted, x, baselineY, maxWidth, maxLines int, text string, textColor color.Color) {
	lines := wrapText(text, maxWidth, maxLines)
	for i, line := range lines {
		drawText(img, x, baselineY+i*14, line, textColor)
	}
}

func measureTextWidth(text string) int {
	return measureTextWidthWithFace(text, basicfont.Face7x13)
}

func measureTextWidthWithFace(text string, face font.Face) int {
	return font.MeasureString(face, text).Round()
}

func mustLoadFontFace(ttf []byte, size float64) font.Face {
	fontData, err := opentype.Parse(ttf)
	if err != nil {
		panic(err)
	}
	face, err := opentype.NewFace(fontData, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		panic(err)
	}
	return face
}

func formatPlayer(name, rank string) string {
	if name == "" {
		name = "Unknown"
	}
	if rank == "" {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, rank)
}

func playerLabel(stone uint8) string {
	switch stone {
	case black:
		return "Black"
	case white:
		return "White"
	default:
		return "Unknown"
	}
}

func annotationColor(state *boardState, x, y int) uint8 {
	switch state.get(x, y) {
	case black:
		return white
	case white:
		return black
	default:
		return highlight
	}
}

func annotationTextColor(state *boardState, x, y int) color.Color {
	switch state.get(x, y) {
	case black:
		return color.White
	default:
		return color.Black
	}
}

func summarizeComment(comment string, maxLen int) string {
	comment = normalizeInlineText(comment)
	if maxLen <= 0 || len(comment) <= maxLen {
		return comment
	}
	if maxLen <= 3 {
		return comment[:maxLen]
	}
	return comment[:maxLen-3] + "..."
}

func wrapText(text string, maxWidth, maxLines int) []string {
	if text == "" || maxLines <= 0 {
		return nil
	}

	lines := []string{}
	for _, paragraph := range strings.Split(text, "\n") {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}

		current := ""
		for _, word := range strings.Fields(paragraph) {
			candidate := word
			if current != "" {
				candidate = current + " " + word
			}
			if font.MeasureString(basicfont.Face7x13, candidate).Round() <= maxWidth {
				current = candidate
				continue
			}

			if current == "" {
				current = fitText(word, maxWidth)
			}
			lines = append(lines, current)
			if len(lines) == maxLines {
				lines[len(lines)-1] = fitText(lines[len(lines)-1]+"...", maxWidth)
				return lines
			}
			current = word
			if font.MeasureString(basicfont.Face7x13, current).Round() > maxWidth {
				current = fitText(current, maxWidth)
			}
		}

		if current != "" {
			lines = append(lines, current)
			if len(lines) == maxLines {
				return lines
			}
		}
	}

	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	return lines
}

func fitText(text string, maxWidth int) string {
	if font.MeasureString(basicfont.Face7x13, text).Round() <= maxWidth {
		return text
	}
	runes := []rune(text)
	for len(runes) > 0 && font.MeasureString(basicfont.Face7x13, string(runes)).Round() > maxWidth {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

func moveLabel(lastMove *move, moveNumber, boardSize int) string {
	if moveNumber == 0 {
		return "Initial position"
	}
	colorName := "B"
	if lastMove != nil && lastMove.white {
		colorName = "W"
	}
	if lastMove != nil && lastMove.pass {
		return fmt.Sprintf("Move %d %s pass", moveNumber, colorName)
	}
	if lastMove == nil {
		return fmt.Sprintf("Move %d", moveNumber)
	}
	return fmt.Sprintf("Move %d %s %s%d", moveNumber, colorName, columnLabel(lastMove.x), boardLabelY(lastMove.y, boardSize))
}

func boardOriginX() int {
	return coordMargin + stoneDiameter/2
}

func boardOriginY() int {
	return infoHeight + coordMargin + stoneDiameter/2
}

func boardOriginYForLayout(layout renderLayout) int {
	layout = layout.normalized()
	return layout.infoHeight + coordMargin + stoneDiameter/2
}

func boardLabelY(y, boardSize int) int {
	return boardSize - y
}

func columnLabel(i int) string {
	if i < 0 {
		return "?"
	}
	letter := 'A' + i
	if letter >= 'I' {
		letter++
	}
	if letter > 'Z' {
		return strconv.Itoa(i + 1)
	}
	return string(rune(letter))
}

func starPoints(boardSize int) []point {
	switch boardSize {
	case 19:
		return cartesianPoints([]int{3, 9, 15})
	case 13:
		return cartesianPoints([]int{3, 6, 9})
	case 9:
		return cartesianPoints([]int{2, 4, 6})
	default:
		return nil
	}
}

func cartesianPoints(values []int) []point {
	points := make([]point, 0, len(values)*len(values))
	for _, y := range values {
		for _, x := range values {
			points = append(points, point{x: x, y: y})
		}
	}
	return points
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func neighbors(x, y int) []point {
	return []point{
		{x: x - 1, y: y},
		{x: x + 1, y: y},
		{x: x, y: y - 1},
		{x: x, y: y + 1},
	}
}

func rootNode(g *sgf.GameTree) (*sgf.Node, error) {
	if len(g.Nodes) == 0 {
		return nil, fmt.Errorf("game tree has no nodes")
	}
	return g.Nodes[0], nil
}

func propertyValue(node *sgf.Node, ident string) string {
	values := propertyValues(node, ident)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func propertyValues(node *sgf.Node, ident string) []string {
	for _, p := range node.Properties {
		if p.Ident == ident {
			return p.Values
		}
	}
	return nil
}

func dist(x1, y1, x2, y2 int) int {
	x := x2 - x1
	if x < 0 {
		x = -x
	}
	y := y2 - y1
	if y < 0 {
		y = -y
	}
	h := float64(x*x + y*y)
	sq := math.Sqrt(h)
	return int(sq)
}
