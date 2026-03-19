package main

import (
	"image"
	"testing"
)

func TestRecommendationCenterDotUsesToPlayColor(t *testing.T) {
	makeCfg := func() renderConfig {
		return renderConfig{
			analysis: &analysisSeries{
				frames: []positionAnalysis{
					{
						topMoves: []analysisMove{
							{
								move:    "aa",
								x:       0,
								y:       0,
								pass:    false,
								visits:  100,
								winrate: 0.5,
							},
						},
					},
				},
			},
			currentFrame: 0,
		}
	}

	tests := []struct {
		name   string
		toPlay uint8
		want   uint8
	}{
		{name: "BlackToPlay", toPlay: black, want: black},
		{name: "WhiteToPlay", toPlay: white, want: white},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := newBoardState(19)
			state.toPlay = tt.toPlay

			img := image.NewPaletted(image.Rect(0, 0, 320, 320), palette)
			fill(img, background)

			cfg := makeCfg()
			drawAnalysisRecommendations(img, state, cfg)

			centerX := boardOriginX()
			centerY := boardOriginYForLayout(cfg.layout.normalized())
			got := img.ColorIndexAt(centerX, centerY)
			if got != tt.want {
				t.Fatalf("center dot color = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestDrawAnalysisPanelRendersSummaryWhenCurrentFrameIsOutOfRange(t *testing.T) {
	img := image.NewPaletted(image.Rect(0, 0, 900, compactInfoHeight+side(19)+2*coordMargin+analysisHeight), palette)
	fill(img, background)

	cfg := renderConfig{
		layout:       renderLayout{infoHeight: compactInfoHeight, analysisHeight: analysisHeight},
		currentFrame: 3,
		summaryFrame: true,
		analysis: &analysisSeries{
			frames: []positionAnalysis{
				{winrate: 0.5},
			},
			summary: &analysisSummary{
				phases: []phaseSummary{
					{label: "Opening"},
					{label: "Middlegame"},
					{label: "Endgame"},
				},
				categories: []categorySummary{
					{label: "Good"},
				},
				totalMoves:   1,
				topMoveCount: 3,
			},
		},
	}

	drawAnalysisPanel(img, cfg)

	foundSummaryInk := false
	panelTop := img.Bounds().Dy() - analysisHeight
	for y := panelTop; y < img.Bounds().Dy() && !foundSummaryInk; y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			colorIndex := img.ColorIndexAt(x, y)
			if colorIndex != background && colorIndex != gridLine {
				foundSummaryInk = true
				break
			}
		}
	}
	if !foundSummaryInk {
		t.Fatal("expected summary panel to render visible content even when currentFrame is out of range")
	}
}
