package main

import (
	"fmt"
	"strings"
)

type analysisSummary struct {
	phases          []phaseSummary
	categories      []categorySummary
	totalMoves      int
	topMoveCount    int
	blackMoveCount  int
	whiteMoveCount  int
	blackLoss       lossStats
	whiteLoss       lossStats
	blackHitStreak  int
	whiteHitStreak  int
	bestBlackPhase  phaseSnapshot
	bestWhitePhase  phaseSnapshot
	worstBlackPhase phaseSnapshot
	worstWhitePhase phaseSnapshot
	biggestSwing    moveSnapshot
	worstBlack      moveSnapshot
	worstWhite      moveSnapshot
	topBlunders     []moveSnapshot
}

type phaseSummary struct {
	label string
	black moveQualityCounter
	white moveQualityCounter
}

type moveQualityCounter struct {
	total int
	hits  int
}

type categorySummary struct {
	label string
	black int
	white int
}

type lossStats struct {
	total float64
	count int
}

type moveSnapshot struct {
	white      bool
	moveNumber int
	move       string
	label      string
	gap        float64
	loss       float64
	valid      bool
}

type phaseSnapshot struct {
	label string
	rate  float64
	hits  int
	total int
	valid bool
}

func buildAnalysisSummaryFromSpecs(specs []frameSpec, analysis *analysisSeries) *analysisSummary {
	if analysis == nil || len(analysis.frames) == 0 {
		return nil
	}

	phases := []phaseSummary{
		{label: "Opening"},
		{label: "Middlegame"},
		{label: "Endgame"},
	}
	categories := []categorySummary{
		{label: "Brilliant"},
		{label: "Excellent"},
		{label: "Good"},
		{label: "OK"},
		{label: "Inaccuracy"},
		{label: "Mistake"},
		{label: "Blunder"},
	}

	totalFrames := minInt(len(specs), len(analysis.frames))
	moveIndexes := make([]int, 0, totalFrames)
	for i := 0; i < totalFrames; i++ {
		if specs[i].current != nil && specs[i].current.move != nil {
			moveIndexes = append(moveIndexes, i)
		}
	}

	summary := &analysisSummary{
		phases:     phases,
		categories: categories,
		totalMoves: len(moveIndexes),
	}
	fixedTopMoves := analysis.cacheMeta != nil && analysis.cacheMeta.TopMoves > 0
	if fixedTopMoves {
		summary.topMoveCount = analysis.cacheMeta.TopMoves
	}
	blackStreak := 0
	whiteStreak := 0
	for moveOrdinal, frameIndex := range moveIndexes {
		action := specs[frameIndex].current
		frame := analysis.frames[frameIndex]
		phaseIdx := phaseIndex(moveOrdinal, len(moveIndexes))
		categoryIdx := categoryIndex(frame)
		counter := &summary.phases[phaseIdx].black
		categoryCounter := &summary.categories[categoryIdx].black
		streak := &blackStreak
		lossStat := &summary.blackLoss
		worst := &summary.worstBlack
		if action.move.white {
			counter = &summary.phases[phaseIdx].white
			categoryCounter = &summary.categories[categoryIdx].white
			streak = &whiteStreak
			lossStat = &summary.whiteLoss
			worst = &summary.worstWhite
			summary.whiteMoveCount++
		} else {
			summary.blackMoveCount++
		}
		counter.total++
		if frame.recommendationHit() {
			counter.hits++
			*streak++
		} else {
			*streak = 0
		}
		if action.move.white {
			if *streak > summary.whiteHitStreak {
				summary.whiteHitStreak = *streak
			}
		} else if *streak > summary.blackHitStreak {
			summary.blackHitStreak = *streak
		}
		*categoryCounter = *categoryCounter + 1
		if !fixedTopMoves && len(frame.topMoves) > summary.topMoveCount {
			summary.topMoveCount = len(frame.topMoves)
		}
		if frame.lossKnown {
			lossStat.total += frame.moveLoss
			lossStat.count++
			if !worst.valid || frame.winrateGap > worst.gap {
				worst.white = action.move.white
				worst.moveNumber = specs[frameIndex].moveNumber
				if worst.moveNumber == 0 {
					worst.moveNumber = moveOrdinal + 1
				}
				worst.move = frame.playedMove
				worst.label = qualityLabelForGap(frame.winrateGap)
				worst.gap = frame.winrateGap
				worst.loss = frame.moveLoss
				worst.valid = true
			}
			if !summary.biggestSwing.valid || frame.moveLoss > summary.biggestSwing.loss {
				summary.biggestSwing = moveSnapshot{
					white:      action.move.white,
					moveNumber: moveOrdinal + 1,
					move:       frame.playedMove,
					label:      qualityLabelForGap(frame.winrateGap),
					gap:        frame.winrateGap,
					loss:       frame.moveLoss,
					valid:      true,
				}
				if specs[frameIndex].moveNumber > 0 {
					summary.biggestSwing.moveNumber = specs[frameIndex].moveNumber
				}
			}
			summary.topBlunders = appendTopBlunder(summary.topBlunders, moveSnapshot{
				white:      action.move.white,
				moveNumber: firstNonZero(specs[frameIndex].moveNumber, moveOrdinal+1),
				move:       frame.playedMove,
				label:      qualityLabelForGap(frame.winrateGap),
				gap:        frame.winrateGap,
				loss:       frame.moveLoss,
				valid:      true,
			}, 3)
		}
	}
	summary.bestBlackPhase = bestPhaseSnapshot(summary.phases, false)
	summary.bestWhitePhase = bestPhaseSnapshot(summary.phases, true)
	summary.worstBlackPhase = worstPhaseSnapshot(summary.phases, false)
	summary.worstWhitePhase = worstPhaseSnapshot(summary.phases, true)

	return summary
}

func phaseIndex(i, total int) int {
	if total <= 0 {
		return 0
	}
	switch {
	case i < total/3:
		return 0
	case i < 2*total/3:
		return 1
	default:
		return 2
	}
}

func categoryIndex(frame positionAnalysis) int {
	gap := frame.winrateGap
	switch {
	case gap < -0.01:
		return 0
	case gap <= 0.01:
		return 1
	case gap <= 0.03:
		return 2
	case gap <= 0.07:
		return 3
	case gap <= 0.12:
		return 4
	case gap <= 0.20:
		return 5
	default:
		return 6
	}
}

func qualityLabelForGap(gap float64) string {
	switch categoryIndex(positionAnalysis{winrateGap: gap}) {
	case 0:
		return "Brilliant"
	case 1:
		return "Excellent"
	case 2:
		return "Good"
	case 3:
		return "OK"
	case 4:
		return "Inaccuracy"
	case 5:
		return "Mistake"
	default:
		return "Blunder"
	}
}

func (s *analysisSummary) maxCategoryCount() int {
	if s == nil {
		return 0
	}
	best := 0
	for _, category := range s.categories {
		if category.black > best {
			best = category.black
		}
		if category.white > best {
			best = category.white
		}
	}
	return best
}

func (p positionAnalysis) recommendationHit() bool {
	if p.playedMove == "" {
		return false
	}
	if p.playedHitKnown {
		return p.playedHit
	}
	for _, move := range p.topMoves {
		if move.move == p.playedMove {
			return true
		}
	}
	return false
}

func (c moveQualityCounter) rate() float64 {
	if c.total == 0 {
		return 0
	}
	return float64(c.hits) / float64(c.total)
}

func (c moveQualityCounter) label() string {
	if c.total == 0 {
		return "--"
	}
	return fmt.Sprintf("%d/%d", c.hits, c.total)
}

func (l lossStats) average() float64 {
	if l.count == 0 {
		return 0
	}
	return l.total / float64(l.count)
}

func bestPhaseSnapshot(phases []phaseSummary, white bool) phaseSnapshot {
	best := phaseSnapshot{}
	for _, phase := range phases {
		counter := phase.black
		if white {
			counter = phase.white
		}
		if counter.total == 0 {
			continue
		}
		current := phaseSnapshot{
			label: phase.label,
			rate:  counter.rate(),
			hits:  counter.hits,
			total: counter.total,
			valid: true,
		}
		if !best.valid || current.rate > best.rate || (current.rate == best.rate && current.total > best.total) {
			best = current
		}
	}
	return best
}

func worstPhaseSnapshot(phases []phaseSummary, white bool) phaseSnapshot {
	worst := phaseSnapshot{}
	for _, phase := range phases {
		counter := phase.black
		if white {
			counter = phase.white
		}
		if counter.total == 0 {
			continue
		}
		current := phaseSnapshot{
			label: phase.label,
			rate:  counter.rate(),
			hits:  counter.hits,
			total: counter.total,
			valid: true,
		}
		if !worst.valid || current.rate < worst.rate || (current.rate == worst.rate && current.total > worst.total) {
			worst = current
		}
	}
	return worst
}

func verdictForSide(summary *analysisSummary, white bool) string {
	if summary == nil {
		return ""
	}
	side := "Black"
	avgLoss := summary.blackLoss.average()
	bestPhase := summary.bestBlackPhase
	hitStreak := summary.blackHitStreak
	if white {
		side = "White"
		avgLoss = summary.whiteLoss.average()
		bestPhase = summary.bestWhitePhase
		hitStreak = summary.whiteHitStreak
	}

	phaseText := "no clear best phase"
	if bestPhase.valid {
		phaseText = "best in " + strings.ToLower(bestPhase.label)
	}

	switch {
	case avgLoss <= 1.0 && hitStreak >= 2:
		return fmt.Sprintf("%s stayed very solid, with %s.", side, phaseText)
	case avgLoss <= 2.5:
		return fmt.Sprintf("%s was mostly steady, %s.", side, phaseText)
	case avgLoss <= 4.0:
		return fmt.Sprintf("%s had a mixed game, but was %s.", side, phaseText)
	default:
		return fmt.Sprintf("%s had several costly misses, even though it was %s.", side, phaseText)
	}
}

func phasePressureVerdict(summary *analysisSummary) string {
	if summary == nil {
		return ""
	}
	return fmt.Sprintf(
		"Phase pressure: B weakest in %s | W weakest in %s",
		formatPhaseLabel(summary.worstBlackPhase),
		formatPhaseLabel(summary.worstWhitePhase),
	)
}

func formatPhaseLabel(snapshot phaseSnapshot) string {
	if !snapshot.valid {
		return "n/a"
	}
	return snapshot.label
}

func appendTopBlunder(existing []moveSnapshot, candidate moveSnapshot, limit int) []moveSnapshot {
	if !candidate.valid || limit <= 0 {
		return existing
	}
	existing = append(existing, candidate)
	for i := len(existing) - 1; i > 0; i-- {
		if existing[i].loss > existing[i-1].loss {
			existing[i], existing[i-1] = existing[i-1], existing[i]
		}
	}
	if len(existing) > limit {
		existing = existing[:limit]
	}
	return existing
}

func firstNonZero(v, fallback int) int {
	if v != 0 {
		return v
	}
	return fallback
}
