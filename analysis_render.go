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
	for i, move := range current.topMoves {
		if move.pass || !state.inBounds(move.x, move.y) || state.get(move.x, move.y) != background {
			continue
		}
		centerX := boardOriginX() + move.x*stoneDiameter
		centerY := boardOriginYForLayout(layout) + move.y*stoneDiameter
		label := recommendationLabel(move, state.toPlay)
		colorIndex := recommendationColor(opponentWinrateForMove(move, state.toPlay))
		radius := recommendationRadius(i)
		drawGhostAura(img, centerX, centerY, radius+4, radius+10, colorIndex, i)
		drawGhostStone(img, centerX, centerY, radius, colorIndex)
		drawCircleOutline(img, centerX, centerY, radius-1, ghostOutlineColor(colorIndex))
		drawCenteredText(img, centerX, centerY+5, label, ghostLabelColor(colorIndex))
		dotRadius := maxInt(2, radius/7)
		toPlay := state.toPlay
		drawCircleOutline(img, centerX, centerY, dotRadius+2, state.opponentOf(toPlay))
		drawFilledDot(img, centerX, centerY, dotRadius, toPlay)
		if i < 3 {
			drawRecommendationRankBadge(img, centerX-radius+4, centerY-radius+8, i+1, colorIndex)
		}
	}
}

func drawAnalysisPanel(img *image.Paletted, cfg renderConfig) {
	layout := cfg.layout.normalized()
	if layout.analysisHeight <= 0 {
		return
	}
	panelTop := img.Bounds().Dy() - layout.analysisHeight
	panelLeft := textPadding
	panelRight := img.Bounds().Dx() - textPadding - 1
	panelBottom := img.Bounds().Dy() - textPadding - 1

	fillRect(img, panelLeft+1, panelTop+3, panelRight-1, panelBottom-1, background)
	drawRectOutline(img, panelLeft, panelTop+2, panelRight, panelBottom, gridLine)

	if cfg.summaryFrame && cfg.analysis.summary != nil {
		drawAnalysisSummaryPanel(img, panelLeft, panelTop, panelRight, panelBottom, cfg.analysis.summary)
		return
	}

	current := cfg.analysis.frameAt(cfg.currentFrame)
	if current == nil {
		return
	}

	statsText := fmt.Sprintf(
		"KataGo | %s | WR %.1f%% | Score %s | Visits %d",
		kataGoViewLabel(cfg.katagoView),
		current.displayWinrate(cfg.katagoView)*100,
		formatScoreLead(current.displayScoreLead(cfg.katagoView), cfg.katagoView),
		current.visits,
	)
	drawText(img, panelLeft+6, panelTop+18, fitText(statsText, panelRight-panelLeft-168), color.Black)
	drawCurrentMoveQualityBadge(img, panelRight-154, panelTop+18, current)

	detailText := formatMoveDetail(current)
	if detailText != "" {
		drawText(img, panelLeft+6, panelTop+34, fitText(detailText, panelRight-panelLeft-12), color.Black)
	}

	recommendText := recommendedMovesSummary(current.topMoves)
	if recommendText != "" {
		drawText(img, panelLeft+6, panelTop+50, fitText(recommendText, panelRight-panelLeft-12), color.Black)
	}
	drawRecommendationLegend(img, panelRight-214, panelTop+50)

	chartLeft := panelLeft + 6
	chartWidth := panelRight - chartLeft - 6
	winrateTop := panelTop + 68
	chartHeight := maxInt(46, (panelBottom-winrateTop-16)/2-10)
	scoreTop := winrateTop + chartHeight + 24

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
		p0 := clampPointToChart(smoothed[i-1], left+1, right-1, top+1, bottom-1)
		p1 := clampPointToChart(smoothed[i], left+1, right-1, top+1, bottom-1)
		drawLine(img, p0.x, p0.y, p1.x, p1.y, lineColor)
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
	radiusSquared := radius * radius
	for x := centerX - radius; x <= centerX+radius; x++ {
		for y := centerY - radius; y <= centerY+radius; y++ {
			if squaredDistance(x, y, centerX, centerY) <= radiusSquared {
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
		parts = append(parts, fmt.Sprintf("%s:%s %dvis", labels[i], loc, move.visits))
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
	if current.lossKnown {
		parts = append(parts, fmt.Sprintf("Loss: %.1f pts", current.moveLoss))
		parts = append(parts, fmt.Sprintf("WR gap: %.1fpp", current.winrateGap*100))
		parts = append(parts, "Quality: "+qualityLabelForGap(current.winrateGap))
	}
	return strings.Join(parts, " | ")
}

func currentMoveHighlightColor(cfg renderConfig) uint8 {
	current := cfg.analysis.frameAt(cfg.currentFrame)
	if current == nil || !current.lossKnown {
		return highlight
	}
	return gapColorIndex(current.winrateGap)
}

func gapColorIndex(gap float64) uint8 {
	switch {
	case gap < -0.01:
		return analysisBlue
	case gap <= 0.01:
		return analysisGreen
	case gap <= 0.03:
		return analysisGreen
	case gap <= 0.07:
		return analysisYellow
	case gap <= 0.12:
		return analysisOrange
	case gap <= 0.20:
		return highlight
	default:
		return analysisRed
	}
}

func opponentWinrateForMove(move analysisMove, nextPlayer uint8) float64 {
	opponent := uint8(black)
	if nextPlayer == black {
		opponent = white
	}
	return winrateForPlayer(move.winrate, opponent)
}

func recommendationLabel(move analysisMove, nextPlayer uint8) string {
	opponent := "B"
	if nextPlayer == black {
		opponent = "W"
	}
	return fmt.Sprintf("%s%.0f", opponent, opponentWinrateForMove(move, nextPlayer)*100)
}

func recommendationColor(opponentWinrate float64) uint8 {
	switch {
	case opponentWinrate < 0.35:
		return analysisGreen
	case opponentWinrate < 0.45:
		return analysisBlue
	case opponentWinrate < 0.55:
		return analysisYellow
	case opponentWinrate < 0.65:
		return analysisOrange
	default:
		return analysisRed
	}
}

func drawGhostStone(img *image.Paletted, centerX, centerY, radius int, colorIndex uint8) {
	outerSquared := radius * radius
	innerRadius := maxInt(0, radius-1)
	innerSquared := innerRadius * innerRadius
	coreRadius := radius / 3
	coreSquared := coreRadius * coreRadius
	for x := centerX - radius; x <= centerX+radius; x++ {
		for y := centerY - radius; y <= centerY+radius; y++ {
			d2 := squaredDistance(x, y, centerX, centerY)
			if d2 > outerSquared {
				continue
			}
			if d2 >= innerSquared || (x+y)%2 == 0 || d2 <= coreSquared {
				img.SetColorIndex(x, y, colorIndex)
			}
		}
	}
}

func drawGhostAura(img *image.Paletted, centerX, centerY, innerRadius, outerRadius int, colorIndex uint8, rank int) {
	spacing := 3 + minInt(rank, 2)
	innerSquared := innerRadius * innerRadius
	outerSquared := outerRadius * outerRadius
	for x := centerX - outerRadius; x <= centerX+outerRadius; x++ {
		for y := centerY - outerRadius; y <= centerY+outerRadius; y++ {
			d2 := squaredDistance(x, y, centerX, centerY)
			if d2 < innerSquared || d2 > outerSquared {
				continue
			}
			if (x+y+d2)%spacing == 0 {
				img.SetColorIndex(x, y, colorIndex)
			}
		}
	}
}

func ghostOutlineColor(colorIndex uint8) uint8 {
	if colorIndex == analysisYellow {
		return black
	}
	return black
}

func ghostLabelColor(colorIndex uint8) color.Color {
	switch colorIndex {
	case analysisYellow:
		return color.Black
	default:
		return color.White
	}
}

func drawAnalysisSummaryPanel(img *image.Paletted, panelLeft, panelTop, panelRight, panelBottom int, summary *analysisSummary) {
	drawText(img, panelLeft+6, panelTop+18, "Game Summary", color.Black)
	drawText(img, panelLeft+6, panelTop+34, fmt.Sprintf("Top %d match rate by phase | Moves %d", maxInt(1, summary.topMoveCount), summary.totalMoves), color.Black)
	drawTextWithColorIndex(img, panelRight-172, panelTop+34, "Blue: Black", analysisBlue)
	drawTextWithColorIndex(img, panelRight-84, panelTop+34, "Orange: White", analysisOrange)
	drawSummaryStatCard(img, panelLeft+6, panelTop+42, 160, "Avg loss", fmt.Sprintf("B %.1f  W %.1f", summary.blackLoss.average(), summary.whiteLoss.average()), analysisGreen)
	drawSummaryStatCard(img, panelLeft+174, panelTop+42, 168, "Best hit streak", fmt.Sprintf("B %d  W %d", summary.blackHitStreak, summary.whiteHitStreak), analysisBlue)
	drawSummaryStatCard(img, panelLeft+350, panelTop+42, 194, "Largest swing", biggestSwingSummary(summary), analysisRed)

	phaseTop := panelTop + 68
	phaseWidth := (panelRight - panelLeft - 24) / 3
	for i, phase := range summary.phases {
		left := panelLeft + 6 + i*(phaseWidth+6)
		right := left + phaseWidth
		drawRectOutline(img, left, phaseTop, right, phaseTop+52, gridLine)
		drawText(img, left+6, phaseTop+16, phase.label, color.Black)
		drawSummaryRateRow(img, left+6, phaseTop+32, right-left-12, "B", phase.black, analysisBlue)
		drawSummaryRateRow(img, left+6, phaseTop+46, right-left-12, "W", phase.white, analysisOrange)
	}

	categoryTop := phaseTop + 66
	drawText(img, panelLeft+6, categoryTop, "Move quality comparison", color.Black)
	drawQualityScaleLegend(img, panelRight-274, categoryTop)
	barLeft := panelLeft + 88
	barRight := panelRight - 8
	rowTop := categoryTop + 8
	maxCount := maxInt(1, summary.maxCategoryCount())
	for i, category := range summary.categories {
		y := rowTop + i*13
		drawText(img, panelLeft+6, y+9, category.label, color.Black)
		drawDualComparisonBar(img, barLeft, y, barRight-barLeft, 8, category.black, category.white, maxCount)
	}
	drawSummaryFooter(img, panelLeft+6, panelBottom, panelRight-panelLeft-12, summary)
}

func drawSummaryRateRow(img *image.Paletted, left, baselineY, width int, label string, counter moveQualityCounter, colorIndex uint8) {
	drawText(img, left, baselineY, label, color.Black)
	barLeft := left + 14
	barWidth := maxInt(24, width-68)
	barTop := baselineY - 10
	barBottom := barTop + 7
	drawRectOutline(img, barLeft, barTop, barLeft+barWidth, barBottom, gridLine)
	fillWidth := int(math.Round(counter.rate() * float64(barWidth-1)))
	if fillWidth > 0 {
		fillRect(img, barLeft+1, barTop+1, barLeft+fillWidth, barBottom-1, colorIndex)
	}
	drawText(img, barLeft+barWidth+6, baselineY, fmt.Sprintf("%.0f%% %s", counter.rate()*100, counter.label()), color.Black)
}

func drawDualComparisonBar(img *image.Paletted, left, top, width, height, blackCount, whiteCount, maxCount int) {
	mid := left + width/2
	half := width / 2
	blackWidth := int(math.Round(float64(blackCount) / float64(maxCount) * float64(half-4)))
	whiteWidth := int(math.Round(float64(whiteCount) / float64(maxCount) * float64(half-4)))
	drawRectOutline(img, left, top, left+width, top+height, gridLine)
	if blackWidth > 0 {
		fillRect(img, mid-blackWidth, top+1, mid-1, top+height-1, analysisBlue)
	}
	if whiteWidth > 0 {
		fillRect(img, mid+1, top+1, mid+whiteWidth, top+height-1, analysisOrange)
	}
	for y := top + 1; y < top+height; y++ {
		img.SetColorIndex(mid, y, gridLine)
	}
	if blackCount > 0 {
		drawText(img, left+2, top+8, strconv.Itoa(blackCount), color.White)
	}
	if whiteCount > 0 {
		drawText(img, left+width-18, top+8, strconv.Itoa(whiteCount), color.Black)
	}
}

func drawArrowLine(img *image.Paletted, x0, y0, x1, y1 int, colorIndex uint8) {
	drawLine(img, x0, y0, x1, y1, colorIndex)
	angle := math.Atan2(float64(y1-y0), float64(x1-x0))
	headLen := 10.0
	leftX := x1 - int(math.Round(headLen*math.Cos(angle-math.Pi/7)))
	leftY := y1 - int(math.Round(headLen*math.Sin(angle-math.Pi/7)))
	rightX := x1 - int(math.Round(headLen*math.Cos(angle+math.Pi/7)))
	rightY := y1 - int(math.Round(headLen*math.Sin(angle+math.Pi/7)))
	drawLine(img, x1, y1, leftX, leftY, colorIndex)
	drawLine(img, x1, y1, rightX, rightY, colorIndex)
}

func drawRecommendationLegend(img *image.Paletted, left, baselineY int) {
	drawTextWithColorIndex(img, left, baselineY, "Green", analysisGreen)
	drawText(img, left+34, baselineY, "low opp WR", color.Black)
	drawTextWithColorIndex(img, left+106, baselineY, "Red", analysisRed)
	drawText(img, left+128, baselineY, "high opp WR", color.Black)
}

func drawCurrentMoveQualityBadge(img *image.Paletted, left, baselineY int, current *positionAnalysis) {
	if current == nil || !current.lossKnown {
		return
	}
	label := qualityLabelForGap(current.winrateGap)
	colorIndex := gapColorIndex(current.winrateGap)
	width := 132
	top := baselineY - 12
	bottom := top + 14
	drawRectOutline(img, left, top, left+width, bottom, colorIndex)
	fillRect(img, left+1, top+1, left+14, bottom-1, colorIndex)
	drawTextWithColorIndex(img, left+20, baselineY, label, colorIndex)
}

func drawMoveTag(img *image.Paletted, centerX, centerY int, text string, colorIndex uint8, alignLeft, below bool) {
	width := measureTextWidth(text) + 10
	left := centerX + 10
	if alignLeft {
		left = centerX - width - 10
	}
	top := centerY - 16
	if below {
		top = centerY + 6
	}
	bottom := top + 14
	drawRectOutline(img, left, top, left+width, bottom, colorIndex)
	fillRect(img, left+1, top+1, left+width-1, bottom-1, background)
	drawTextWithColorIndex(img, left+5, top+11, text, colorIndex)
}

func drawRecommendationRankBadge(img *image.Paletted, centerX, centerY, rank int, colorIndex uint8) {
	drawFilledDot(img, centerX, centerY, 7, ghostOutlineColor(colorIndex))
	drawCenteredText(img, centerX, centerY+4, strconv.Itoa(rank), color.White)
}

func recommendationRadius(rank int) int {
	base := stoneDiameter/2 - 2
	switch rank {
	case 0:
		return base
	case 1:
		return base - 3
	case 2:
		return base - 5
	default:
		return maxInt(12, base-6-rank)
	}
}

func drawQualityScaleLegend(img *image.Paletted, left, baselineY int) {
	items := []struct {
		label      string
		colorIndex uint8
	}{
		{label: "Brilliant", colorIndex: analysisBlue},
		{label: "Good", colorIndex: analysisGreen},
		{label: "OK", colorIndex: analysisYellow},
		{label: "Mistake", colorIndex: analysisOrange},
		{label: "Blunder", colorIndex: analysisRed},
	}
	x := left
	for _, item := range items {
		fillRect(img, x, baselineY-8, x+6, baselineY-2, item.colorIndex)
		drawText(img, x+10, baselineY, item.label, color.Black)
		x += 10 + measureTextWidth(item.label) + 12
	}
}

func drawSummaryStatCard(img *image.Paletted, left, top, width int, title, value string, colorIndex uint8) {
	bottom := top + 18
	drawRectOutline(img, left, top, left+width, bottom, gridLine)
	fillRect(img, left+1, top+1, left+4, bottom-1, colorIndex)
	drawText(img, left+9, top+11, title, color.Black)
	drawTextWithColorIndex(img, left+70, top+11, fitText(value, width-76), colorIndex)
}

func drawSummaryFooter(img *image.Paletted, left, panelBottom, maxWidth int, summary *analysisSummary) {
	y := panelBottom - 62
	drawWrappedText(img, left, y, maxWidth, 1, verdictForSide(summary, false), color.Black)
	y += 14
	drawWrappedText(img, left, y, maxWidth, 1, verdictForSide(summary, true), color.Black)
	y += 14
	drawWrappedText(img, left, y, maxWidth, 1, phasePressureVerdict(summary), color.Black)
	y += 14
	drawWrappedText(img, left, y, maxWidth, 2, topBlundersSummary(summary), color.Black)
}

func worstMoveSummary(summary *analysisSummary) string {
	bestSide := "B"
	best := summary.worstBlack
	if summary.worstWhite.valid && (!best.valid || summary.worstWhite.gap > best.gap) {
		bestSide = "W"
		best = summary.worstWhite
	}
	if !best.valid {
		return "n/a"
	}
	moveText := best.move
	if moveText == "" {
		moveText = "?"
	}
	return fmt.Sprintf("%s%d %s %.1fpt", bestSide, best.moveNumber, moveText, best.loss)
}

func biggestSwingSummary(summary *analysisSummary) string {
	if !summary.biggestSwing.valid {
		return "n/a"
	}
	side := "B"
	if summary.biggestSwing.white {
		side = "W"
	}
	moveText := summary.biggestSwing.move
	if moveText == "" {
		moveText = "?"
	}
	return fmt.Sprintf("%s%d %s %.1fpt", side, summary.biggestSwing.moveNumber, moveText, summary.biggestSwing.loss)
}

func topBlundersSummary(summary *analysisSummary) string {
	if summary == nil || len(summary.topBlunders) == 0 {
		return "Top blunders: n/a"
	}
	parts := make([]string, 0, len(summary.topBlunders))
	for _, item := range summary.topBlunders {
		side := "B"
		if item.white {
			side = "W"
		}
		moveText := item.move
		if moveText == "" {
			moveText = "?"
		}
		parts = append(parts, fmt.Sprintf("%s%d %s %.1fpt", side, item.moveNumber, moveText, item.loss))
	}
	return "Top blunders: " + strings.Join(parts, " | ")
}

func bestPhaseSummary(summary *analysisSummary) string {
	return fmt.Sprintf(
		"Best phase: B %s | W %s",
		formatPhaseSnapshot(summary.bestBlackPhase),
		formatPhaseSnapshot(summary.bestWhitePhase),
	)
}

func formatPhaseSnapshot(snapshot phaseSnapshot) string {
	if !snapshot.valid {
		return "n/a"
	}
	return fmt.Sprintf("%s %.0f%%", snapshot.label, snapshot.rate*100)
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

func clampPointToChart(p point, minX, maxX, minY, maxY int) point {
	if p.x < minX {
		p.x = minX
	}
	if p.x > maxX {
		p.x = maxX
	}
	if p.y < minY {
		p.y = minY
	}
	if p.y > maxY {
		p.y = maxY
	}
	return p
}
