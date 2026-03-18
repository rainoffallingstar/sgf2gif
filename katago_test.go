package main

import (
	"os"
	"testing"
)

func TestParseArgsAcceptsKataGoFlags(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{
		"sgf2gif",
		"--katago-strength", "strong",
		"--katago-bin", "/tmp/katago",
		"--katago-model", "/tmp/model.bin.gz",
		"--katago-config", "/tmp/analysis.cfg",
		"--katago-threads", "4",
		"--katago-top-moves", "5",
		"in.sgf",
		"out.gif",
	}

	opts, err := parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}

	if !opts.enableKataGo {
		t.Fatal("enableKataGo = false, want true")
	}
	if opts.katagoStrength != "strong" {
		t.Fatalf("katagoStrength = %q, want %q", opts.katagoStrength, "strong")
	}
	if opts.katagoVisits != 1000 || opts.katagoThreads != 4 || opts.katagoTopMoves != 5 {
		t.Fatalf("unexpected KataGo numeric flags: %#v", opts)
	}
}

func TestParseArgsExplicitVisitsOverrideStrength(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{
		"sgf2gif",
		"--katago-strength", "monster",
		"--katago-visits", "7",
		"in.sgf",
		"out.gif",
	}

	opts, err := parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if opts.katagoVisits != 7 {
		t.Fatalf("katagoVisits = %d, want 7", opts.katagoVisits)
	}
}

func TestKataGoVisitsForStrength(t *testing.T) {
	tests := map[string]int{
		"fast":    100,
		"strong":  1000,
		"monster": 10000,
	}
	for input, want := range tests {
		got, err := katagoVisitsForStrength(input)
		if err != nil {
			t.Fatalf("katagoVisitsForStrength(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("katagoVisitsForStrength(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestSelectKataGoAssetPrefersEigen(t *testing.T) {
	release := &githubRelease{
		TagName: "v1.16.4",
		Assets: []githubReleaseAsset{
			{Name: "katago-v1.16.4-opencl-linux-x64.zip", URL: "opencl"},
			{Name: "katago-v1.16.4-eigenavx2-linux-x64.zip", URL: "eigenavx2"},
		},
	}

	asset, err := selectKataGoAsset(release, "linux", "amd64")
	if err != nil {
		t.Fatalf("selectKataGoAsset returned error: %v", err)
	}
	if asset.URL != "eigenavx2" {
		t.Fatalf("selected asset URL = %q, want %q", asset.URL, "eigenavx2")
	}
}

func TestExtractModelURLsPrefersLatestUniqueLinks(t *testing.T) {
	html := `
		<a href="https://media.katagotraining.org/uploaded/networks/models/kata1-b18c384nbt-latest.bin.gz">b18</a>
		<a href="https://media.katagotraining.org/uploaded/networks/models/kata1-b18c384nbt-latest.bin.gz">dup</a>
		<a href="https://media.katagotraining.org/uploaded/networks/models/kata1-b28c512nbt-latest.bin.gz">b28</a>
	`

	urls := extractModelURLs(html)
	if len(urls) != 2 {
		t.Fatalf("got %d model urls, want 2", len(urls))
	}
	if urls[0] != "https://media.katagotraining.org/uploaded/networks/models/kata1-b18c384nbt-latest.bin.gz" {
		t.Fatalf("unexpected first model url: %q", urls[0])
	}
}

func TestParseGTPMove(t *testing.T) {
	x, y, pass, err := parseGTPMove("Q16", 19)
	if err != nil {
		t.Fatalf("parseGTPMove returned error: %v", err)
	}
	if pass || x != 15 || y != 3 {
		t.Fatalf("unexpected parsed move: x=%d y=%d pass=%v", x, y, pass)
	}
}

func TestSelectRenderLayoutAddsAnalysisPanel(t *testing.T) {
	layout := selectRenderLayout(nil, true)
	if layout.analysisHeight != analysisHeight {
		t.Fatalf("analysisHeight = %d, want %d", layout.analysisHeight, analysisHeight)
	}
}
