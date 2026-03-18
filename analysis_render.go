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

	fillRect(img, panelLeft+1, panelTop+3, panelRight-1, panelBottom-1, background)
	drawRectOutline(img, panelLeft, panelTop+2, panelRight, panelBottom, gridLine)

	statsText := fmt.Sprintf(
		"KataGo | %s | WR %.1f%% | Score %s | Visits %d",
		kataGoViewLabel(cfg.katagoView),
		current.displayWinrate(cfg.katagoView)*100,
		formatScoreLead(current.displayScoreLead(cfg.katagoView), cfg.katagoView),
		current.visits,
	)
	drawText(img, panelLeft+6, panelTop+18, fitText(statsText, panelRight-panelLeft-12), color.Black)

	detailText := formatMoveDetail(current)
	if detailText != "" {
		drawText(img, panelLeft+6, panelTop+34, fitText(detailText, panelRight-panelLeft-12), color.Black)
	}

	recommendText := recommendedMovesSummary(current.topMoves)
	if recommendText != "" {
		drawText(img, panelLeft+6, panelTop+50, fitText(recommendText, panelRight-panelLeft-12), color.Black)
	}

	chartLeft := panelLeft + 6
	chartWidth := panelRight - chartLeft - 6
	winrateTop := panelTop + 62
	scoreTop := panelTop + 114
	chartHeight := 40

	drawTextWithColorIndex(img, chartLeft, winrateTop-4, "Winrate", analysisBlue)
	drawLineChart(
		img,
		chartLeft,
		winrateTop,
		chartWidth,
		chartHeight,
		cfg.analysis.winrates(cfg.katagoView),
		0,
		1,
		0.5,
		cfg.currentFrame,
		analysisBlue,
	)

	drawTextWithColorIndex(img, chartLeft, scoreTop-4, "Score lead", analysisGreen)
	minScore, maxScore := cfg.analysis.scoreRange(cfg.katagoView)
	drawLineChart(
		img,
		chartLeft,
		scoreTop,
		chartWidth,
		chartHeight,
		cfg.analysis.scoreLeads(cfg.katagoView),
		minScore,
		maxScore,
		0,
		cfg.currentFrame,
		analysisGreen,
	)
}

func (s *analysisSeries) winrates(view string) []float64 {
	if s == nil {
		return nil
	}
	values := make([]float64, 0, len(s.frames))
	for _, frame := range s.frames {
		values = append(values, frame.displayWinrate(view))
	}
	return values
}

func (s *analysisSeries) scoreLeads(view string) []float64 {
	if s == nil {
		return nil
	}
	values := make([]float64, 0, len(s.frames))
	for _, frame := range s.frames {
		values = append(values, frame.displayScoreLead(view))
	}
	return values
}

func (s *analysisSeries) scoreRange(view string) (float64, float64) {
	values := s.scoreLeads(view)
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

func (p positionAnalysis) displayWinrate(view string) float64 {
	if view == "white" {
		return 1 - p.winrate
	}
	return p.winrate
}

func (p positionAnalysis) displayScoreLead(view string) float64 {
	if view == "white" {
		return -p.scoreLead
	}
	return p.scoreLead
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

	points := make([]point, 0, len(values))
	for i := range values {
		x := left + 1 + i*(width-3)/(len(values)-1)
		y := chartYForValue(values[i], top, height, minValue, maxValue)
		points = append(points, point{x: x, y: y})
	}
	smoothed := smoothChartPoints(points)
	for i := 1; i < len(smoothed); i++ {
		drawLine(img, smoothed[i-1].x, smoothed[i-1].y, smoothed[i].x, smoothed[i].y, lineColor)
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

func fillRect(img *image.Paletted, left, top, right, bottom int, colorIndex uint8) {
	for y := top; y <= bottom; y++ {
		for x := left; x <= right; x++ {
			img.SetColorIndex(x, y, colorIndex)
		}
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

func kataGoViewLabel(view string) string {
	if view == "white" {
		return "White perspective"
	}
	return "Black perspective"
}

func formatScoreLead(score float64, view string) string {
	if math.Abs(score) < 0.05 {
		return "Even"
	}
	leader := "B"
	trailer := "W"
	if view == "white" {
		leader = "W"
		trailer = "B"
	}
	if score > 0 {
		return leader + "+" + strconv.FormatFloat(score, 'f', 1, 64)
	}
	return trailer + "+" + strconv.FormatFloat(math.Abs(score), 'f', 1, 64)
}

func formatMoveDetail(current *positionAnalysis) string {
	parts := []string{}
	if current.playedMove != "" {
		parts = append(parts, "Played: "+current.playedMove)
	}
	if current.bestMove != "" {
		parts = append(parts, "Best: "+current.bestMove)
	}
	if current.lossKnown {
		parts = append(parts, fmt.Sprintf("Loss: %.1f pts", current.moveLoss))
	} else if current.playedMove != "" {
		parts = append(parts, "Loss: n/a")
	}
	return strings.Join(parts, " | ")
}

func smoothChartPoints(points []point) []point {
	if len(points) < 3 {
		return points
	}
	smoothed := make([]point, 0, len(points)*8)
	smoothed = append(smoothed, points[0])
	for i := 0; i < len(points)-1; i++ {
		p0 := points[maxInt(i-1, 0)]
		p1 := points[i]
		p2 := points[i+1]
		p3 := points[minInt(i+2, len(points)-1)]
		for step := 1; step <= 8; step++ {
			t := float64(step) / 8.0
			x := catmullRom(float64(p0.x), float64(p1.x), float64(p2.x), float64(p3.x), t)
			y := catmullRom(float64(p0.y), float64(p1.y), float64(p2.y), float64(p3.y), t)
			smoothed = append(smoothed, point{x: int(math.Round(x)), y: int(math.Round(y))})
		}
	}
	return smoothed
}

func catmullRom(p0, p1, p2, p3, t float64) float64 {
	return 0.5 * ((2 * p1) +
		(-p0+p2)*t +
		(2*p0-5*p1+4*p2-p3)*t*t +
		(-p0+3*p1-3*p2+p3)*t*t*t)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
