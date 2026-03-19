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
