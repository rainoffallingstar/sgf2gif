package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
)

func drawAnalysisRecommendations(img *image.Paletted, state *boardState, cfg renderConfig) {
	current := cfg.analysis.frameAt(cfg.currentFrame)
	if current == nil || len(current.topMoves) == 0 {
		return
	}

	layout := cfg.layout.normalized()
	labels := []string{"A", "B", "C", "D", "E"}
	colors := []uint8{analysisBlue, analysisGreen, analysisOrange, analysisBlue, analysisGreen}
	for i, move := range current.topMoves {
		if move.pass || i >= len(labels) || !state.inBounds(move.x, move.y) || state.get(move.x, move.y) != background {
			continue
		}
		centerX := boardOriginX() + move.x*stoneDiameter
		centerY := boardOriginYForLayout(layout) + move.y*stoneDiameter
		drawCircleOutline(img, centerX, centerY, stoneDiameter/3, colors[i%len(colors)])
		drawCenteredText(img, centerX, centerY+5, labels[i], color.Black)
	}
}

func drawAnalysisPanel(img *image.Paletted, cfg renderConfig) {
	layout := cfg.layout.normalized()
	if layout.analysisHeight <= 0 {
		return
	}
	current := cfg.analysis.frameAt(cfg.currentFrame)
	if current == nil {
		return
	}

	panelTop := img.Bounds().Dy() - layout.analysisHeight
	panelLeft := textPadding
	panelRight := img.Bounds().Dx() - textPadding - 1
	panelBottom := img.Bounds().Dy() - textPadding - 1

	drawRectOutline(img, panelLeft, panelTop+2, panelRight, panelBottom, gridLine)

	statsText := fmt.Sprintf("KataGo | WR %.1f%% | Score %s | Visits %d", current.winrate*100, formatScoreLead(current.scoreLead), current.visits)
	drawText(img, panelLeft+6, panelTop+18, fitText(statsText, panelRight-panelLeft-12), color.Black)

	recommendText := recommendedMovesSummary(current.topMoves)
	if recommendText != "" {
		drawText(img, panelLeft+6, panelTop+34, fitText(recommendText, panelRight-panelLeft-12), color.Black)
	}

	chartLeft := panelLeft + 6
	chartWidth := panelRight - chartLeft - 6
	winrateTop := panelTop + 42
	scoreTop := panelTop + 92
	chartHeight := 34

	drawTextWithColorIndex(img, chartLeft, winrateTop-4, "Winrate", analysisBlue)
	drawLineChart(
		img,
		chartLeft,
		winrateTop,
		chartWidth,
		chartHeight,
		cfg.analysis.winrates(),
		0,
		1,
		0.5,
		cfg.currentFrame,
		analysisBlue,
	)

	drawTextWithColorIndex(img, chartLeft, scoreTop-4, "Score lead", analysisGreen)
	minScore, maxScore := cfg.analysis.scoreRange()
	drawLineChart(
		img,
		chartLeft,
		scoreTop,
		chartWidth,
		chartHeight,
		cfg.analysis.scoreLeads(),
		minScore,
		maxScore,
		0,
		cfg.currentFrame,
		analysisGreen,
	)
}

func (s *analysisSeries) winrates() []float64 {
	if s == nil {
		return nil
	}
	values := make([]float64, 0, len(s.frames))
	for _, frame := range s.frames {
		values = append(values, frame.winrate)
	}
	return values
}

func (s *analysisSeries) scoreLeads() []float64 {
	if s == nil {
		return nil
	}
	values := make([]float64, 0, len(s.frames))
	for _, frame := range s.frames {
		values = append(values, frame.scoreLead)
	}
	return values
}

func (s *analysisSeries) scoreRange() (float64, float64) {
	values := s.scoreLeads()
	if len(values) == 0 {
		return -1, 1
	}
	minValue := values[0]
	maxValue := values[0]
	for _, value := range values[1:] {
		if value < minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}
	if minValue > 0 {
		minValue = 0
	}
	if maxValue < 0 {
		maxValue = 0
	}
	if minValue == maxValue {
		minValue--
		maxValue++
	}
	padding := math.Max(0.5, (maxValue-minValue)*0.1)
	return minValue - padding, maxValue + padding
}

func drawLineChart(img *image.Paletted, left, top, width, height int, values []float64, minValue, maxValue, baseline float64, currentIndex int, lineColor uint8) {
	if width <= 2 || height <= 2 {
		return
	}
	right := left + width - 1
	bottom := top + height - 1
	drawRectOutline(img, left, top, right, bottom, gridLine)

	if maxValue <= minValue {
		maxValue = minValue + 1
	}
	if baseline >= minValue && baseline <= maxValue {
		y := chartYForValue(baseline, top, height, minValue, maxValue)
		for x := left + 1; x < right; x++ {
			img.SetColorIndex(x, y, analysisGray)
		}
	}
	if len(values) == 0 {
		return
	}
	if len(values) == 1 {
		x := left + width/2
		y := chartYForValue(values[0], top, height, minValue, maxValue)
		drawFilledDot(img, x, y, 2, lineColor)
		return
	}

	prevX := left + 1
	prevY := chartYForValue(values[0], top, height, minValue, maxValue)
	for i := 1; i < len(values); i++ {
		x := left + 1 + i*(width-3)/(len(values)-1)
		y := chartYForValue(values[i], top, height, minValue, maxValue)
		drawLine(img, prevX, prevY, x, y, lineColor)
		prevX = x
		prevY = y
	}

	if currentIndex >= 0 && currentIndex < len(values) {
		x := left + 1 + currentIndex*(width-3)/(len(values)-1)
		y := chartYForValue(values[currentIndex], top, height, minValue, maxValue)
		drawFilledDot(img, x, y, 2, highlight)
	}
}

func chartYForValue(value float64, top, height int, minValue, maxValue float64) int {
	usable := height - 3
	if usable < 1 {
		return top + height/2
	}
	ratio := (value - minValue) / (maxValue - minValue)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	return top + 1 + int(math.Round((1-ratio)*float64(usable)))
}

func drawRectOutline(img *image.Paletted, left, top, right, bottom int, colorIndex uint8) {
	for x := left; x <= right; x++ {
		img.SetColorIndex(x, top, colorIndex)
		img.SetColorIndex(x, bottom, colorIndex)
	}
	for y := top; y <= bottom; y++ {
		img.SetColorIndex(left, y, colorIndex)
		img.SetColorIndex(right, y, colorIndex)
	}
}

func drawFilledDot(img *image.Paletted, centerX, centerY, radius int, colorIndex uint8) {
	for x := centerX - radius; x <= centerX+radius; x++ {
		for y := centerY - radius; y <= centerY+radius; y++ {
			if dist(x, y, centerX, centerY) <= radius {
				img.SetColorIndex(x, y, colorIndex)
			}
		}
	}
}

func drawTextWithColorIndex(img *image.Paletted, x, baselineY int, text string, colorIndex uint8) {
	drawText(img, x, baselineY, text, palette[colorIndex])
}

func recommendedMovesSummary(moves []analysisMove) string {
	if len(moves) == 0 {
		return ""
	}

	labels := []string{"A", "B", "C", "D", "E"}
	parts := make([]string, 0, len(moves))
	for i, move := range moves {
		if i >= len(labels) {
			break
		}
		loc := "pass"
		if !move.pass {
			loc = move.move
		}
		parts = append(parts, labels[i]+":"+loc)
	}
	return "Next: " + strings.Join(parts, "  ")
}

func formatScoreLead(score float64) string {
	if math.Abs(score) < 0.05 {
		return "Even"
	}
	if score > 0 {
		return "B+" + strconv.FormatFloat(score, 'f', 1, 64)
	}
	return "W+" + strconv.FormatFloat(math.Abs(score), 'f', 1, 64)
}
