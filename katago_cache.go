package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/toikarin/sgf"
)

const (
	katagoCacheRootProp  = "KTCACHE"
	katagoCacheDiagProp  = "KTDIAG"
	katagoCacheMetaProp  = "KTMETA"
	katagoCacheNodeProp  = "KT"
	katagoCacheVersionV1 = "sgf2gif-katago-v1"
	katagoCacheVersionV2 = "sgf2gif-katago-v2"
	katagoCacheVersion   = katagoCacheVersionV2
)

type cachedPositionAnalysis struct {
	Winrate       float64              `json:"winrate"`
	ScoreLead     float64              `json:"scoreLead"`
	Visits        int                  `json:"visits"`
	TopMoves      []cachedAnalysisMove `json:"topMoves,omitempty"`
	PlayedMove    string               `json:"playedMove,omitempty"`
	BestMove      string               `json:"bestMove,omitempty"`
	MoveLoss      float64              `json:"moveLoss,omitempty"`
	LossKnown     bool                 `json:"lossKnown,omitempty"`
	BestWinrate   float64              `json:"bestWinrate,omitempty"`
	ActualWinrate float64              `json:"actualWinrate,omitempty"`
	WinrateGap    float64              `json:"winrateGap,omitempty"`
}

type cachedAnalysisMove struct {
	Move      string  `json:"move"`
	X         int     `json:"x"`
	Y         int     `json:"y"`
	Pass      bool    `json:"pass,omitempty"`
	Visits    int     `json:"visits,omitempty"`
	Order     int     `json:"order,omitempty"`
	Winrate   float64 `json:"winrate,omitempty"`
	ScoreLead float64 `json:"scoreLead,omitempty"`
}

type katagoDiagnosticsMetadata struct {
	Summary    string `json:"summary,omitempty"`
	Report     string `json:"report,omitempty"`
	Platform   string `json:"platform,omitempty"`
	Preference string `json:"preference,omitempty"`
	Binary     string `json:"binary,omitempty"`
	Model      string `json:"model,omitempty"`
	Config     string `json:"config,omitempty"`
	Fallback   string `json:"fallback,omitempty"`
}

type katagoCacheMetadata struct {
	GeneratedBy string `json:"generatedBy,omitempty"`
	Backend     string `json:"backend,omitempty"`
	MaxVisits   int    `json:"maxVisits,omitempty"`
	Threads     int    `json:"threads,omitempty"`
	Workers     int    `json:"workers,omitempty"`
	TopMoves    int    `json:"topMoves,omitempty"`
}

func cachedAnalysisFromGame(g *sgf.GameTree, boardSize int, variationPath []int) (*analysisSeries, error) {
	root, err := rootNode(g)
	if err != nil {
		return nil, err
	}
	cacheVersion, err := validateKataGoCacheVersion(root)
	if err != nil {
		return nil, err
	}
	if cacheVersion == "" {
		return nil, nil
	}

	nodes, err := selectedCacheNodesFromGame(g, boardSize, variationPath)
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, nil
	}

	frames := make([]positionAnalysis, 0, len(nodes))
	for _, node := range nodes {
		payload := propertyValue(node, katagoCacheNodeProp)
		if payload == "" {
			return nil, nil
		}
		var cached cachedPositionAnalysis
		if err := json.Unmarshal([]byte(payload), &cached); err != nil {
			return nil, fmt.Errorf("failed to parse cached KataGo analysis: %w", err)
		}
		frame := positionAnalysis{
			winrate:       cached.Winrate,
			scoreLead:     cached.ScoreLead,
			visits:        cached.Visits,
			playedMove:    cached.PlayedMove,
			bestMove:      cached.BestMove,
			moveLoss:      cached.MoveLoss,
			lossKnown:     cached.LossKnown,
			bestWinrate:   cached.BestWinrate,
			actualWinrate: cached.ActualWinrate,
			winrateGap:    cached.WinrateGap,
		}
		for _, move := range cached.TopMoves {
			frame.topMoves = append(frame.topMoves, analysisMove{
				move:      move.Move,
				x:         move.X,
				y:         move.Y,
				pass:      move.Pass,
				visits:    move.Visits,
				order:     move.Order,
				winrate:   move.Winrate,
				scoreLead: move.ScoreLead,
			})
		}
		frames = append(frames, frame)
	}

	diagnostics, err := decodeKataGoDiagnostics(propertyValue(root, katagoCacheDiagProp))
	if err != nil {
		return nil, err
	}
	cacheMeta, err := decodeKataGoCacheMetadata(propertyValue(root, katagoCacheMetaProp))
	if err != nil {
		return nil, err
	}

	return &analysisSeries{
		frames:      frames,
		diagnostics: diagnostics,
		cacheMeta:   cacheMeta,
	}, nil
}

func validateKataGoCacheVersion(root *sgf.Node) (string, error) {
	version := strings.TrimSpace(propertyValue(root, katagoCacheRootProp))
	switch version {
	case "":
		return "", nil
	case katagoCacheVersionV1, katagoCacheVersionV2:
		return version, nil
	default:
		return "", fmt.Errorf("unsupported KataGo cache version: %s", version)
	}
}

type katagoCacheAction string

const (
	katagoCacheActionUse  katagoCacheAction = "use"
	katagoCacheActionRun  katagoCacheAction = "run"
	katagoCacheActionFail katagoCacheAction = "fail"
)

func canReuseCachedAnalysis(cached *analysisSeries, opts katagoOptions) (bool, string) {
	if cached == nil {
		return false, "no cached analysis found"
	}
	if cached.cacheMeta == nil {
		return false, "cache metadata unavailable"
	}
	if cached.cacheMeta.MaxVisits < opts.maxVisits {
		return false, fmt.Sprintf("cache maxVisits=%d is lower than requested %d", cached.cacheMeta.MaxVisits, opts.maxVisits)
	}
	if cached.cacheMeta.TopMoves < opts.topMoves {
		return false, fmt.Sprintf("cache topMoves=%d is lower than requested %d", cached.cacheMeta.TopMoves, opts.topMoves)
	}
	requestedBackend := normalizeBackendLabel(opts.backend)
	cachedBackend := normalizeStoredBackendLabel(cached.cacheMeta.Backend)
	if requestedBackend != "auto" {
		switch cachedBackend {
		case "":
			return false, fmt.Sprintf("cache backend unavailable for explicit request %s", requestedBackend)
		case unknownKataGoBackend:
			return false, fmt.Sprintf("cache backend unknown for explicit request %s", requestedBackend)
		case requestedBackend:
		default:
			return false, fmt.Sprintf("cache backend=%s differs from requested %s", cachedBackend, requestedBackend)
		}
	}
	if cachedBackend == "" {
		cachedBackend = "unspecified"
	}
	return true, fmt.Sprintf("cache maxVisits=%d, topMoves=%d, backend=%s", cached.cacheMeta.MaxVisits, cached.cacheMeta.TopMoves, cachedBackend)
}

func determineKataGoCacheAction(cached *analysisSeries, opts katagoOptions, forceRefresh, cacheOnly bool) (katagoCacheAction, string) {
	if forceRefresh {
		return katagoCacheActionRun, "forced by --katago-refresh"
	}
	reuse, reason := canReuseCachedAnalysis(cached, opts)
	if reuse {
		return katagoCacheActionUse, reason
	}
	if cacheOnly {
		return katagoCacheActionFail, reason
	}
	return katagoCacheActionRun, reason
}

func annotatedSGFForVariation(c *sgf.Collection, boardSize int, variationPath []int, analysis *analysisSeries) ([]byte, error) {
	if analysis == nil {
		return nil, nil
	}

	clone, err := sgf.ParseSgf(c.Sgf(sgf.NoNewLinesSgfFormat))
	if err != nil {
		return nil, err
	}
	game, err := firstGame(clone)
	if err != nil {
		return nil, err
	}
	nodes, err := selectedCacheNodesFromGame(game, boardSize, variationPath)
	if err != nil {
		return nil, err
	}
	if len(nodes) != len(analysis.frames) {
		return nil, fmt.Errorf("cached analysis frame count mismatch: %d nodes vs %d frames", len(nodes), len(analysis.frames))
	}

	root, err := rootNode(game)
	if err != nil {
		return nil, err
	}
	setProperty(root, katagoCacheRootProp, katagoCacheVersion)
	if diagnostics := strings.TrimSpace(analysis.diagnostics); diagnostics != "" {
		encodedDiagnostics, err := encodeKataGoDiagnostics(diagnostics)
		if err != nil {
			return nil, err
		}
		setProperty(root, katagoCacheDiagProp, encodedDiagnostics)
		setProperty(root, "C", appendKataGoDiagnosticsComment(propertyValue(root, "C"), diagnostics))
	}
	if analysis.cacheMeta != nil {
		encodedMeta, err := encodeKataGoCacheMetadata(analysis.cacheMeta)
		if err != nil {
			return nil, err
		}
		setProperty(root, katagoCacheMetaProp, encodedMeta)
	}

	for i, node := range nodes {
		payload, err := encodeCachedPositionAnalysis(analysis.frames[i])
		if err != nil {
			return nil, err
		}
		setProperty(node, katagoCacheNodeProp, payload)
	}

	return []byte(clone.Sgf(sgf.DefaultSgfFormat)), nil
}

func appendKataGoDiagnosticsComment(existing, diagnostics string) string {
	diagnostics = strings.TrimSpace(diagnostics)
	if diagnostics == "" {
		return existing
	}
	summary := summarizeKataGoDiagnostics(diagnostics)
	if summary == "" {
		return existing
	}
	section := "sgf2gif KataGo summary\n" + summary
	existing = strings.TrimSpace(existing)
	if existing == "" {
		return section
	}
	if strings.Contains(existing, section) {
		return existing
	}
	return existing + "\n\n" + section
}

func summarizeKataGoDiagnostics(report string) string {
	metadata := parseKataGoDiagnostics(report)
	parts := make([]string, 0, 5)
	if metadata.Platform != "" {
		parts = append(parts, "platform="+metadata.Platform)
	}
	if metadata.Preference != "" {
		parts = append(parts, "pref="+metadata.Preference)
	}
	if metadata.Binary != "" {
		parts = append(parts, "bin="+metadata.Binary)
	}
	if metadata.Model != "" {
		parts = append(parts, "model="+metadata.Model)
	}
	if metadata.Config != "" {
		parts = append(parts, "config="+metadata.Config)
	}
	if len(parts) == 0 {
		return ""
	}
	summary := "KataGo: " + strings.Join(parts, "; ")
	if metadata.Fallback != "" {
		summary += "\nFallback: " + metadata.Fallback
	}
	return summary
}

func parseKataGoDiagnostics(report string) katagoDiagnosticsMetadata {
	metadata := katagoDiagnosticsMetadata{
		Report: strings.TrimSpace(report),
	}
	platform := ""
	preference := ""
	binary := ""
	model := ""
	config := ""
	fallback := ""

	lines := strings.Split(report, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "Platform: "):
			platform = strings.TrimPrefix(line, "Platform: ")
		case strings.HasPrefix(line, "KataGo backend preference: "):
			preference = strings.TrimPrefix(line, "KataGo backend preference: ")
		case strings.HasPrefix(line, "KataGo binary: "):
			binary = compactKataGoDiagnosticStatus(strings.TrimPrefix(line, "KataGo binary: "))
		case strings.HasPrefix(line, "KataGo backend fallback: "):
			fallback = strings.TrimPrefix(line, "KataGo backend fallback: ")
		case strings.HasPrefix(line, "KataGo model: "):
			model = compactKataGoDiagnosticStatus(strings.TrimPrefix(line, "KataGo model: "))
		case strings.HasPrefix(line, "KataGo config: "):
			config = compactKataGoDiagnosticStatus(strings.TrimPrefix(line, "KataGo config: "))
		}
	}
	metadata.Platform = platform
	metadata.Preference = preference
	metadata.Binary = binary
	metadata.Model = model
	metadata.Config = config
	metadata.Fallback = fallback
	return metadata
}

func encodeKataGoDiagnostics(report string) (string, error) {
	metadata := parseKataGoDiagnostics(report)
	metadata.Summary = summarizeKataGoDiagnostics(report)
	data, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func buildKataGoCacheMetadata(opts katagoOptions, resolvedBackend string) *katagoCacheMetadata {
	resolvedBackend = normalizeStoredBackendLabel(resolvedBackend)
	if resolvedBackend == "" {
		resolvedBackend = unknownKataGoBackend
	}
	return &katagoCacheMetadata{
		GeneratedBy: katagoCacheVersion,
		Backend:     resolvedBackend,
		MaxVisits:   opts.maxVisits,
		Threads:     opts.threads,
		Workers:     opts.workers,
		TopMoves:    opts.topMoves,
	}
}

func encodeKataGoCacheMetadata(meta *katagoCacheMetadata) (string, error) {
	if meta == nil {
		return "", nil
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeKataGoCacheMetadata(value string) (*katagoCacheMetadata, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	var meta katagoCacheMetadata
	if err := json.Unmarshal([]byte(value), &meta); err != nil {
		return nil, fmt.Errorf("failed to parse cached KataGo metadata: %w", err)
	}
	return &meta, nil
}

func decodeKataGoDiagnostics(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, "{") {
		return value, nil
	}
	var metadata katagoDiagnosticsMetadata
	if err := json.Unmarshal([]byte(value), &metadata); err != nil {
		return "", fmt.Errorf("failed to parse cached KataGo diagnostics: %w", err)
	}
	if strings.TrimSpace(metadata.Report) != "" {
		return strings.TrimSpace(metadata.Report), nil
	}
	return strings.TrimSpace(value), nil
}

func compactKataGoDiagnosticStatus(value string) string {
	switch {
	case strings.HasPrefix(value, "existing file at "):
		return "ready"
	case strings.HasPrefix(value, "found on PATH at "):
		return "PATH"
	case strings.HasPrefix(value, "would download "):
		return "download"
	case strings.HasPrefix(value, "not installed;"):
		return "missing"
	case strings.HasPrefix(value, "automatic download is not supported"):
		return "unsupported"
	default:
		return value
	}
}

func selectedCacheNodesFromGame(g *sgf.GameTree, boardSize int, variationPath []int) ([]*sgf.Node, error) {
	nodes, branchDepth, err := selectedCacheNodesFromGameAtDepth(g, boardSize, variationPath, 0, true)
	if err != nil {
		return nil, err
	}
	if branchDepth != len(variationPath) {
		return nil, fmt.Errorf("variation-path has too many entries: %v", variationPath)
	}
	return nodes, nil
}

func selectedCacheNodesFromGameAtDepth(g *sgf.GameTree, boardSize int, variationPath []int, branchDepth int, isRoot bool) ([]*sgf.Node, int, error) {
	ret := []*sgf.Node{}
	for i, node := range g.Nodes {
		action, _, err := actionFromNode(node, boardSize)
		if err != nil {
			return nil, branchDepth, err
		}
		if cacheNodeHasFrameEffect(action, isRoot && i == 0) {
			ret = append(ret, node)
		}
	}

	switch n := len(g.GameTrees); n {
	case 0:
		return ret, branchDepth, nil
	case 1:
		childNodes, consumed, err := selectedCacheNodesFromGameAtDepth(g.GameTrees[0], boardSize, variationPath, branchDepth, false)
		if err != nil {
			return nil, consumed, err
		}
		return append(ret, childNodes...), consumed, nil
	}

	selected := 0
	if branchDepth < len(variationPath) {
		selected = variationPath[branchDepth] - 1
	}
	if selected < 0 || selected >= len(g.GameTrees) {
		return nil, branchDepth, fmt.Errorf("variation index %d out of range at branch %d (choices: %d)", selected+1, branchDepth+1, len(g.GameTrees))
	}

	childNodes, consumed, err := selectedCacheNodesFromGameAtDepth(g.GameTrees[selected], boardSize, variationPath, branchDepth+1, false)
	if err != nil {
		return nil, consumed, err
	}
	return append(ret, childNodes...), consumed, nil
}

func cacheNodeHasFrameEffect(a *action, isRoot bool) bool {
	if a == nil {
		return false
	}
	if a.move != nil || len(a.setups) > 0 || a.toPlay != background || len(a.marks) > 0 || len(a.labels) > 0 {
		return true
	}
	if isRoot {
		return false
	}
	return a.comment != ""
}

func annotatedSGFPath(outputPath string) string {
	ext := filepath.Ext(outputPath)
	base := outputPath
	if ext != "" {
		base = outputPath[:len(outputPath)-len(ext)]
	}
	return base + ".katago.sgf"
}

func encodeCachedPositionAnalysis(frame positionAnalysis) (string, error) {
	cached := cachedPositionAnalysis{
		Winrate:       frame.winrate,
		ScoreLead:     frame.scoreLead,
		Visits:        frame.visits,
		PlayedMove:    frame.playedMove,
		BestMove:      frame.bestMove,
		MoveLoss:      frame.moveLoss,
		LossKnown:     frame.lossKnown,
		BestWinrate:   frame.bestWinrate,
		ActualWinrate: frame.actualWinrate,
		WinrateGap:    frame.winrateGap,
	}
	for _, move := range frame.topMoves {
		cached.TopMoves = append(cached.TopMoves, cachedAnalysisMove{
			Move:      move.move,
			X:         move.x,
			Y:         move.y,
			Pass:      move.pass,
			Visits:    move.visits,
			Order:     move.order,
			Winrate:   move.winrate,
			ScoreLead: move.scoreLead,
		})
	}
	data, err := json.Marshal(cached)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func selectedEffectNodesFromGame(g *sgf.GameTree, boardSize int, variationPath []int) ([]*sgf.Node, error) {
	nodes, branchDepth, err := selectedEffectNodesFromGameAtDepth(g, boardSize, variationPath, 0)
	if err != nil {
		return nil, err
	}
	if branchDepth != len(variationPath) {
		return nil, fmt.Errorf("variation-path has too many entries: %v", variationPath)
	}
	return nodes, nil
}

func selectedEffectNodesFromGameAtDepth(g *sgf.GameTree, boardSize int, variationPath []int, branchDepth int) ([]*sgf.Node, int, error) {
	ret := []*sgf.Node{}
	for _, node := range g.Nodes {
		_, hasEffect, err := actionFromNode(node, boardSize)
		if err != nil {
			return nil, branchDepth, err
		}
		if hasEffect {
			ret = append(ret, node)
		}
	}

	switch n := len(g.GameTrees); n {
	case 0:
		return ret, branchDepth, nil
	case 1:
		childNodes, consumed, err := selectedEffectNodesFromGameAtDepth(g.GameTrees[0], boardSize, variationPath, branchDepth)
		if err != nil {
			return nil, consumed, err
		}
		return append(ret, childNodes...), consumed, nil
	}

	selected := 0
	if branchDepth < len(variationPath) {
		selected = variationPath[branchDepth] - 1
	}
	if selected < 0 || selected >= len(g.GameTrees) {
		return nil, branchDepth, fmt.Errorf("variation index %d out of range at branch %d (choices: %d)", selected+1, branchDepth+1, len(g.GameTrees))
	}

	childNodes, consumed, err := selectedEffectNodesFromGameAtDepth(g.GameTrees[selected], boardSize, variationPath, branchDepth+1)
	if err != nil {
		return nil, consumed, err
	}
	return append(ret, childNodes...), consumed, nil
}

func setProperty(node *sgf.Node, ident string, values ...string) {
	for i := len(node.Properties) - 1; i >= 0; i-- {
		if node.Properties[i].Ident == ident {
			node.RemovePropertyAt(i)
		}
	}
	if len(values) > 0 {
		node.NewProperty(ident, values...)
	}
}
