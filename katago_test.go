package main

import (
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseArgsAcceptsKataGoFlags(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{
		"sgf2gif",
		"--katago-strength", "strong",
		"--katago-refresh",
		"--katago-backend", "cuda",
		"--katago-bin", "/tmp/katago",
		"--katago-model", "/tmp/model.bin.gz",
		"--katago-config", "/tmp/analysis.cfg",
		"--katago-threads", "4",
		"--katago-workers", "2",
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
	if opts.katagoBackend != "cuda" {
		t.Fatalf("katagoBackend = %q, want %q", opts.katagoBackend, "cuda")
	}
	if !opts.katagoRefresh {
		t.Fatal("katagoRefresh = false, want true")
	}
	if opts.katagoNoCacheWrite {
		t.Fatal("katagoNoCacheWrite = true, want false by default")
	}
	if opts.katagoVisits != 1000 || opts.katagoThreads != 4 || opts.katagoWorkers != 2 || opts.katagoTopMoves != 5 {
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

func TestParseArgsAcceptsKataGoCacheOnlyFlag(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{
		"sgf2gif",
		"--katago-cache-only",
		"in.sgf",
		"out.gif",
	}

	opts, err := parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if !opts.enableKataGo {
		t.Fatal("enableKataGo = false, want true when katago-cache-only is set")
	}
	if !opts.katagoCacheOnly {
		t.Fatal("katagoCacheOnly = false, want true")
	}
}

func TestParseArgsAcceptsNoCacheWriteFlag(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{
		"sgf2gif",
		"--katago-analyze",
		"--katago-no-cache-write",
		"in.sgf",
		"out.gif",
	}

	opts, err := parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if !opts.enableKataGo {
		t.Fatal("enableKataGo = false, want true when katago analysis is enabled")
	}
	if !opts.katagoNoCacheWrite {
		t.Fatal("katagoNoCacheWrite = false, want true")
	}
}

func TestParseArgsRejectsNoCacheWriteWithoutAnalysisContext(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{
		"sgf2gif",
		"--katago-no-cache-write",
		"in.sgf",
		"out.gif",
	}

	_, err := parseArgs()
	if err == nil {
		t.Fatal("parseArgs returned nil error for katago-no-cache-write without analysis context")
	}
	if !strings.Contains(err.Error(), "katago-no-cache-write requires KataGo analysis") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseArgsRejectsRefreshAndCacheOnlyTogether(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{
		"sgf2gif",
		"--katago-refresh",
		"--katago-cache-only",
		"in.sgf",
		"out.gif",
	}

	_, err := parseArgs()
	if err == nil {
		t.Fatal("parseArgs returned nil error for conflicting cache flags")
	}
	if !strings.Contains(err.Error(), "katago-refresh cannot be combined with katago-cache-only") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestShouldWriteKataGoCache(t *testing.T) {
	if shouldWriteKataGoCache(&options{}, nil) {
		t.Fatal("shouldWriteKataGoCache returned true for nil analysis")
	}
	if !shouldWriteKataGoCache(&options{}, &analysisSeries{}) {
		t.Fatal("shouldWriteKataGoCache returned false for normal cache write")
	}
	if shouldWriteKataGoCache(&options{katagoNoCacheWrite: true}, &analysisSeries{}) {
		t.Fatal("shouldWriteKataGoCache returned true when katagoNoCacheWrite is enabled")
	}
}

func TestParseArgsAcceptsKataGoViewFlag(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{
		"sgf2gif",
		"--katago-view", "white",
		"in.sgf",
		"out.gif",
	}

	opts, err := parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if !opts.enableKataGo {
		t.Fatal("enableKataGo = false, want true when katago-view is set")
	}
	if opts.katagoView != "white" {
		t.Fatalf("katagoView = %q, want %q", opts.katagoView, "white")
	}
}

func TestParseArgsAcceptsKataGoDetectOnlyWithoutPositionalArgs(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{
		"sgf2gif",
		"--katago-detect-only",
		"--katago-backend", "cpu",
	}

	opts, err := parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if !opts.katagoDetectOnly {
		t.Fatal("katagoDetectOnly = false, want true")
	}
	if opts.katagoDiagnosticsOut != filepath.Join(defaultKataGoRoot, "diagnostics.txt") {
		t.Fatalf("katagoDiagnosticsOut = %q, want %q", opts.katagoDiagnosticsOut, filepath.Join(defaultKataGoRoot, "diagnostics.txt"))
	}
	if opts.inputPath != "" || opts.outputPath != "" {
		t.Fatalf("detect-only should not require positional paths, got input=%q output=%q", opts.inputPath, opts.outputPath)
	}
}

func TestNormalizeKataGoView(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "", want: "black"},
		{input: "black", want: "black"},
		{input: "WHITE", want: "white"},
		{input: "  white  ", want: "white"},
		{input: "both", wantErr: true},
	}

	for _, tt := range tests {
		got, err := normalizeKataGoView(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("normalizeKataGoView(%q) returned nil error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizeKataGoView(%q) returned error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("normalizeKataGoView(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeKataGoBackend(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "", want: "auto"},
		{input: "auto", want: "auto"},
		{input: "CPU", want: "cpu"},
		{input: " opencl ", want: "opencl"},
		{input: "cuda", want: "cuda"},
		{input: "metal", wantErr: true},
	}

	for _, tt := range tests {
		got, err := normalizeKataGoBackend(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("normalizeKataGoBackend(%q) returned nil error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizeKataGoBackend(%q) returned error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("normalizeKataGoBackend(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestKataGoVisitsForStrength(t *testing.T) {
	tests := map[string]int{
		"mild":    50,
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

func TestSelectKataGoAssetPrefersEigenForCPU(t *testing.T) {
	release := &githubRelease{
		TagName: "v1.16.4",
		Assets: []githubReleaseAsset{
			{Name: "katago-v1.16.4-opencl-linux-x64.zip", URL: "opencl"},
			{Name: "katago-v1.16.4-eigenavx2-linux-x64.zip", URL: "eigenavx2"},
			{Name: "katago-v1.16.4-cuda-linux-x64.zip", URL: "cuda"},
		},
	}

	asset, backend, err := selectKataGoAsset(release, "linux", "amd64", []string{"cpu"})
	if err != nil {
		t.Fatalf("selectKataGoAsset returned error: %v", err)
	}
	if backend != "cpu" {
		t.Fatalf("selected backend = %q, want %q", backend, "cpu")
	}
	if asset.URL != "eigenavx2" {
		t.Fatalf("selected asset URL = %q, want %q", asset.URL, "eigenavx2")
	}
}

func TestSelectKataGoAssetPrefersCUDAWhenRequested(t *testing.T) {
	release := &githubRelease{
		TagName: "v1.16.4",
		Assets: []githubReleaseAsset{
			{Name: "katago-v1.16.4-opencl-linux-x64.zip", URL: "opencl"},
			{Name: "katago-v1.16.4-cuda-linux-x64.zip", URL: "cuda"},
			{Name: "katago-v1.16.4-eigen-linux-x64.zip", URL: "eigen"},
		},
	}

	asset, backend, err := selectKataGoAsset(release, "linux", "amd64", []string{"cuda", "opencl", "cpu"})
	if err != nil {
		t.Fatalf("selectKataGoAsset returned error: %v", err)
	}
	if backend != "cuda" {
		t.Fatalf("selected backend = %q, want %q", backend, "cuda")
	}
	if asset.URL != "cuda" {
		t.Fatalf("selected asset URL = %q, want %q", asset.URL, "cuda")
	}
}

func TestSelectKataGoAssetFallsBackFromOpenCLToCPU(t *testing.T) {
	release := &githubRelease{
		TagName: "v1.16.4",
		Assets: []githubReleaseAsset{
			{Name: "katago-v1.16.4-eigen-linux-x64.zip", URL: "eigen"},
		},
	}

	asset, backend, err := selectKataGoAsset(release, "linux", "amd64", []string{"opencl", "cpu"})
	if err != nil {
		t.Fatalf("selectKataGoAsset returned error: %v", err)
	}
	if backend != "cpu" {
		t.Fatalf("selected backend = %q, want %q", backend, "cpu")
	}
	if asset.URL != "eigen" {
		t.Fatalf("selected asset URL = %q, want %q", asset.URL, "eigen")
	}
}

func TestPreferredKataGoBackendsExplicitOpenCLFallsBackToCPU(t *testing.T) {
	backends, reason, err := preferredKataGoBackends("linux", "opencl")
	if err != nil {
		t.Fatalf("preferredKataGoBackends returned error: %v", err)
	}
	if len(backends) != 2 || backends[0] != "opencl" || backends[1] != "cpu" {
		t.Fatalf("unexpected backends: %#v", backends)
	}
	if reason != "explicit OpenCL backend requested" {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestPreferredKataGoBackendsAutoReportsDetectedSignals(t *testing.T) {
	oldCUDAHome := os.Getenv("CUDA_HOME")
	oldLDLibraryPath := os.Getenv("LD_LIBRARY_PATH")
	defer func() {
		if oldCUDAHome == "" {
			_ = os.Unsetenv("CUDA_HOME")
		} else {
			_ = os.Setenv("CUDA_HOME", oldCUDAHome)
		}
		if oldLDLibraryPath == "" {
			_ = os.Unsetenv("LD_LIBRARY_PATH")
		} else {
			_ = os.Setenv("LD_LIBRARY_PATH", oldLDLibraryPath)
		}
	}()

	if err := os.Setenv("CUDA_HOME", "/tmp/fake-cuda"); err != nil {
		t.Fatalf("Setenv returned error: %v", err)
	}

	tmpDir := t.TempDir()
	cudnnPath := filepath.Join(tmpDir, "libcudnn.so.8")
	if err := os.WriteFile(cudnnPath, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) returned error: %v", cudnnPath, err)
	}
	if err := os.Setenv("LD_LIBRARY_PATH", tmpDir); err != nil {
		t.Fatalf("Setenv returned error: %v", err)
	}

	backends, reason, err := preferredKataGoBackends("linux", "auto")
	if err != nil {
		t.Fatalf("preferredKataGoBackends returned error: %v", err)
	}
	if len(backends) != 3 || backends[0] != "cuda" {
		t.Fatalf("unexpected backends: %#v", backends)
	}
	if !strings.Contains(reason, "CUDA_HOME") {
		t.Fatalf("expected reason to mention CUDA_HOME, got %q", reason)
	}
}

func TestDetectKataGoSetupReportsExistingFiles(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "katago")
	modelPath := filepath.Join(tmpDir, "model.bin.gz")
	configPath := filepath.Join(tmpDir, "analysis.cfg")
	diagnosticsPath := filepath.Join(tmpDir, "diagnostics.txt")
	for _, path := range []string{binPath, modelPath, configPath} {
		if err := os.WriteFile(path, []byte("stub"), 0o755); err != nil {
			t.Fatalf("WriteFile(%q) returned error: %v", path, err)
		}
	}

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe returned error: %v", err)
	}
	os.Stdout = w

	runErr := detectKataGoSetup(katagoOptions{
		binPath:        binPath,
		modelPath:      modelPath,
		configPath:     configPath,
		diagnosticsOut: diagnosticsPath,
		backend:        "cpu",
	})

	_ = w.Close()
	os.Stdout = oldStdout
	output, readErr := io.ReadAll(r)
	_ = r.Close()
	if readErr != nil {
		t.Fatalf("ReadAll returned error: %v", readErr)
	}
	if runErr != nil {
		t.Fatalf("detectKataGoSetup returned error: %v", runErr)
	}

	text := string(output)
	for _, want := range []string{
		"KataGo detect-only mode",
		"KataGo backend preference: cpu -> cpu (explicit CPU backend requested)",
		"KataGo binary: existing file at " + binPath,
		"KataGo model: existing file at " + modelPath,
		"KataGo config: existing file at " + configPath,
		"KataGo diagnostics saved to " + diagnosticsPath,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected output to contain %q, got %q", want, text)
		}
	}
	saved, err := os.ReadFile(diagnosticsPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !strings.Contains(string(saved), "KataGo detect-only mode") {
		t.Fatalf("saved diagnostics missing detect-only header: %q", string(saved))
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

func TestFormatKataGoDuration(t *testing.T) {
	if got := formatKataGoDuration(75 * time.Second); got != "01:15" {
		t.Fatalf("formatKataGoDuration = %q, want %q", got, "01:15")
	}
}

func TestPrintAnalysisProgressIncludesElapsedAndETA(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe returned error: %v", err)
	}
	os.Stdout = w

	printAnalysisProgress(2, 4, time.Now().Add(-30*time.Second))

	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	_ = r.Close()
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	text := string(output)
	if !strings.Contains(text, "elapsed") || !strings.Contains(text, "eta") {
		t.Fatalf("progress output missing elapsed/eta: %q", text)
	}
}

func TestSelectRenderLayoutAddsAnalysisPanel(t *testing.T) {
	layout := selectRenderLayout(nil, true)
	if layout.analysisHeight != analysisHeight {
		t.Fatalf("analysisHeight = %d, want %d", layout.analysisHeight, analysisHeight)
	}
}

func TestBuildKataGoWorkerPlansSplitsQueriesAndThreads(t *testing.T) {
	queries := []katagoAnalysisQuery{
		{ID: "q0"},
		{ID: "q1"},
		{ID: "q2"},
		{ID: "q3"},
		{ID: "q4"},
	}

	plans := buildKataGoWorkerPlans(queries, 2, 5)
	if len(plans) != 2 {
		t.Fatalf("got %d worker plans, want 2", len(plans))
	}
	if len(plans[0].queries) != 3 || len(plans[1].queries) != 2 {
		t.Fatalf("unexpected query split: %#v", plans)
	}
	if plans[0].threads != 3 || plans[1].threads != 2 {
		t.Fatalf("unexpected thread split: %#v", plans)
	}
	if plans[0].queries[0].ID != "q0" || plans[1].queries[0].ID != "q3" {
		t.Fatalf("unexpected query order in plans: %#v", plans)
	}
}

func TestBuildKataGoWorkerPlansCapsWorkersByThreads(t *testing.T) {
	queries := []katagoAnalysisQuery{
		{ID: "q0"},
		{ID: "q1"},
		{ID: "q2"},
	}

	plans := buildKataGoWorkerPlans(queries, 5, 2)
	if len(plans) != 2 {
		t.Fatalf("got %d worker plans, want 2", len(plans))
	}
	if plans[0].threads < 1 || plans[1].threads < 1 {
		t.Fatalf("worker threads must stay positive: %#v", plans)
	}
}

func TestBuildKataGoQueriesAddsDecisionQueriesForPlayedMoves(t *testing.T) {
	initial := newBoardState(9)
	actions := []*action{
		{move: &move{x: 0, y: 0}},
		{comment: "note only"},
		{move: &move{white: true, x: 1, y: 1}},
	}

	specs, err := actionsToFrameSpecs(initial, actions, positionalSuperkoRule)
	if err != nil {
		t.Fatalf("actionsToFrameSpecs returned error: %v", err)
	}

	queries, decisions := buildKataGoQueries(&gameInfo{rules: "Chinese"}, specs, 321)
	if len(queries) != 5 {
		t.Fatalf("got %d queries, want 5", len(queries))
	}
	if len(decisions) != 2 {
		t.Fatalf("got %d decision queries, want 2", len(decisions))
	}

	ids := map[string]bool{}
	for _, query := range queries {
		ids[query.ID] = true
	}
	for _, wantID := range []string{"frame-0000", "frame-0001", "frame-0002", "decision-0000", "decision-0002"} {
		if !ids[wantID] {
			t.Fatalf("missing query id %q in %#v", wantID, ids)
		}
	}

	if got := decisions[0].id(); got != "decision-0000" {
		t.Fatalf("first decision id = %q, want %q", got, "decision-0000")
	}
	if got := moveToGTP(decisions[0].move, decisions[0].before.size); got != "A9" {
		t.Fatalf("first played move = %q, want %q", got, "A9")
	}
	if decisions[0].before.toPlay != black {
		t.Fatalf("first decision toPlay = %v, want black", decisions[0].before.toPlay)
	}

	if got := decisions[1].id(); got != "decision-0002" {
		t.Fatalf("second decision id = %q, want %q", got, "decision-0002")
	}
	if got := moveToGTP(decisions[1].move, decisions[1].before.size); got != "B8" {
		t.Fatalf("second played move = %q, want %q", got, "B8")
	}
	if decisions[1].before.toPlay != white {
		t.Fatalf("second decision toPlay = %v, want white", decisions[1].before.toPlay)
	}
}

func TestPopulateAnalysisFramesIgnoresDecisionResponsesForFrameIndexing(t *testing.T) {
	specs := []frameSpec{
		{state: newBoardState(9)},
	}
	results := map[string]katagoAnalysisResponse{
		frameQueryID(0): {
			ID: frameQueryID(0),
			RootInfo: katagoRootInfo{
				Winrate:   0.61,
				ScoreLead: 2.5,
				Visits:    100,
			},
			MoveInfos: []katagoMoveInfo{
				{Move: "D4", Visits: 90, Order: 0, Winrate: 0.61, ScoreLead: 2.5},
			},
		},
		"decision-0000": {
			ID: "decision-0000",
			RootInfo: katagoRootInfo{
				Winrate:   0.55,
				ScoreLead: 1.0,
				Visits:    100,
			},
			MoveInfos: []katagoMoveInfo{
				{Move: "E5", Visits: 80, Order: 0, Winrate: 0.55, ScoreLead: 1.0},
			},
		},
	}

	frames, err := populateAnalysisFrames(specs, results, katagoOptions{topMoves: 3})
	if err != nil {
		t.Fatalf("populateAnalysisFrames returned error: %v", err)
	}
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if frames[0].visits != 100 {
		t.Fatalf("visits = %d, want 100", frames[0].visits)
	}
	if len(frames[0].topMoves) != 1 {
		t.Fatalf("got %d top moves, want 1", len(frames[0].topMoves))
	}
	if frames[0].topMoves[0].move != "D4" {
		t.Fatalf("top move = %q, want %q", frames[0].topMoves[0].move, "D4")
	}
}

func TestRootScoreForPlayer(t *testing.T) {
	if got := rootScoreForPlayer(3.5, black); got != 3.5 {
		t.Fatalf("rootScoreForPlayer(3.5, black) = %v, want 3.5", got)
	}
	if got := rootScoreForPlayer(3.5, white); got != -3.5 {
		t.Fatalf("rootScoreForPlayer(3.5, white) = %v, want -3.5", got)
	}
}

func TestApplyDecisionAnalysisUsesFrameRootForActualMoveLoss(t *testing.T) {
	frames := []positionAnalysis{
		{scoreLead: 1.2},
	}
	decisions := []decisionQueryRef{
		{
			frameIndex: 0,
			before:     newBoardState(9),
			move:       &move{x: 0, y: 0},
		},
	}
	results := map[string]katagoAnalysisResponse{
		"decision-0000": {
			MoveInfos: []katagoMoveInfo{
				{Move: "D4", ScoreLead: 2.7, Visits: 100, Order: 0},
			},
		},
	}

	applyDecisionAnalysis(frames, decisions, results)

	if frames[0].playedMove != "A9" {
		t.Fatalf("playedMove = %q, want %q", frames[0].playedMove, "A9")
	}
	if frames[0].bestMove != "D4" {
		t.Fatalf("bestMove = %q, want %q", frames[0].bestMove, "D4")
	}
	if !frames[0].lossKnown {
		t.Fatal("lossKnown = false, want true")
	}
	if math.Abs(frames[0].moveLoss-1.5) > 1e-9 {
		t.Fatalf("moveLoss = %v, want 1.5", frames[0].moveLoss)
	}
}
