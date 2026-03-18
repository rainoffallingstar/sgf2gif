package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/toikarin/sgf"
)

const (
	katagoCacheRootProp = "KTCACHE"
	katagoCacheNodeProp = "KT"
	katagoCacheVersion  = "sgf2gif-katago-v1"
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

func cachedAnalysisFromGame(g *sgf.GameTree, boardSize int, variationPath []int) (*analysisSeries, error) {
	nodes, err := selectedEffectNodesFromGame(g, boardSize, variationPath)
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

	return &analysisSeries{frames: frames}, nil
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
	nodes, err := selectedEffectNodesFromGame(game, boardSize, variationPath)
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

	for i, node := range nodes {
		payload, err := encodeCachedPositionAnalysis(analysis.frames[i])
		if err != nil {
			return nil, err
		}
		setProperty(node, katagoCacheNodeProp, payload)
	}

	return []byte(clone.Sgf(sgf.DefaultSgfFormat)), nil
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
