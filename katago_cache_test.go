package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/toikarin/sgf"
)

func TestAnnotatedSGFForVariationRoundTripsCachedAnalysis(t *testing.T) {
	collection, err := sgf.ParseSgf("(;FF[4]GM[1]SZ[9]PB[Black]PW[White];B[aa];W[bb])")
	if err != nil {
		t.Fatalf("ParseSgf returned error: %v", err)
	}

	analysis := &analysisSeries{
		diagnostics: "Platform: linux/amd64\nKataGo backend preference: auto -> cpu (no GPU runtime detected)",
		cacheMeta: &katagoCacheMetadata{
			GeneratedBy: katagoCacheVersionV2,
			Backend:     "cpu",
			MaxVisits:   1000,
			Threads:     2,
			Workers:     1,
			TopMoves:    3,
		},
		frames: []positionAnalysis{
			{
				winrate:    0.60,
				scoreLead:  2.5,
				visits:     100,
				playedMove: "A9",
				bestMove:   "D4",
				moveLoss:   0.7,
				lossKnown:  true,
				topMoves: []analysisMove{
					{move: "D4", x: 3, y: 5, visits: 100, order: 0, winrate: 0.60, scoreLead: 2.5},
				},
			},
			{
				winrate:    0.58,
				scoreLead:  1.8,
				visits:     90,
				playedMove: "B8",
				bestMove:   "C3",
				moveLoss:   0.4,
				lossKnown:  true,
			},
		},
	}

	data, err := annotatedSGFForVariation(collection, 9, nil, analysis)
	if err != nil {
		t.Fatalf("annotatedSGFForVariation returned error: %v", err)
	}
	if !strings.Contains(string(data), katagoCacheRootProp+"["+katagoCacheVersion+"]") {
		t.Fatalf("annotated SGF missing root cache property: %s", string(data))
	}
	if !strings.Contains(string(data), katagoCacheDiagProp+"[{\"summary\":") {
		t.Fatalf("annotated SGF missing diagnostics property: %s", string(data))
	}
	if !strings.Contains(string(data), katagoCacheMetaProp+"[{\"generatedBy\":") {
		t.Fatalf("annotated SGF missing cache metadata property: %s", string(data))
	}
	if !strings.Contains(string(data), "C[sgf2gif KataGo summary") {
		t.Fatalf("annotated SGF missing diagnostics summary comment: %s", string(data))
	}
	if !strings.Contains(string(data), "KataGo: platform=linux/amd64; pref=auto -> cpu (no GPU runtime detected)") {
		t.Fatalf("annotated SGF missing compact diagnostics summary: %s", string(data))
	}
	if strings.Contains(string(data), "KataGo model: existing file at") {
		t.Fatalf("annotated SGF summary comment should omit verbose diagnostics lines: %s", string(data))
	}
	rootCollection, err := sgf.ParseSgf(string(data))
	if err != nil {
		t.Fatalf("ParseSgf root output returned error: %v", err)
	}
	rootGame, err := firstGame(rootCollection)
	if err != nil {
		t.Fatalf("firstGame root output returned error: %v", err)
	}
	root, err := rootNode(rootGame)
	if err != nil {
		t.Fatalf("rootNode returned error: %v", err)
	}
	var diagnostics katagoDiagnosticsMetadata
	if err := json.Unmarshal([]byte(propertyValue(root, katagoCacheDiagProp)), &diagnostics); err != nil {
		t.Fatalf("failed to parse KTDIAG json: %v", err)
	}
	if diagnostics.Platform != "linux/amd64" {
		t.Fatalf("diagnostics platform = %q, want %q", diagnostics.Platform, "linux/amd64")
	}
	if diagnostics.Preference != "auto -> cpu (no GPU runtime detected)" {
		t.Fatalf("diagnostics preference = %q", diagnostics.Preference)
	}
	if diagnostics.Summary == "" || diagnostics.Report == "" {
		t.Fatalf("diagnostics missing summary/report: %#v", diagnostics)
	}
	var cacheMeta katagoCacheMetadata
	if err := json.Unmarshal([]byte(propertyValue(root, katagoCacheMetaProp)), &cacheMeta); err != nil {
		t.Fatalf("failed to parse KTMETA json: %v", err)
	}
	if cacheMeta.MaxVisits != 1000 || cacheMeta.TopMoves != 3 {
		t.Fatalf("unexpected cache meta: %#v", cacheMeta)
	}

	cachedCollection, err := sgf.ParseSgf(string(data))
	if err != nil {
		t.Fatalf("ParseSgf cached output returned error: %v", err)
	}
	cachedGame, err := firstGame(cachedCollection)
	if err != nil {
		t.Fatalf("firstGame cached output returned error: %v", err)
	}
	loaded, err := cachedAnalysisFromGame(cachedGame, 9, nil)
	if err != nil {
		t.Fatalf("cachedAnalysisFromGame returned error: %v", err)
	}
	if loaded == nil || len(loaded.frames) != 2 {
		t.Fatalf("loaded frames = %#v", loaded)
	}
	if loaded.diagnostics != analysis.diagnostics {
		t.Fatalf("loaded diagnostics = %q, want %q", loaded.diagnostics, analysis.diagnostics)
	}
	if loaded.cacheMeta == nil {
		t.Fatal("loaded cacheMeta is nil")
	}
	if loaded.cacheMeta.GeneratedBy != katagoCacheVersionV2 || loaded.cacheMeta.Backend != "cpu" {
		t.Fatalf("unexpected loaded cacheMeta: %#v", loaded.cacheMeta)
	}
	if loaded.frames[0].bestMove != "D4" {
		t.Fatalf("bestMove = %q, want %q", loaded.frames[0].bestMove, "D4")
	}
	if !loaded.frames[0].lossKnown || loaded.frames[0].moveLoss != 0.7 {
		t.Fatalf("unexpected cached loss: %#v", loaded.frames[0])
	}
	if loaded.frames[1].playedMove != "B8" {
		t.Fatalf("playedMove = %q, want %q", loaded.frames[1].playedMove, "B8")
	}
}

func TestAnnotatedSGFForVariationPreservesExistingRootComment(t *testing.T) {
	collection, err := sgf.ParseSgf("(;FF[4]GM[1]SZ[9]C[Original root note];B[aa])")
	if err != nil {
		t.Fatalf("ParseSgf returned error: %v", err)
	}

	analysis := &analysisSeries{
		diagnostics: "Platform: linux/amd64",
		frames: []positionAnalysis{
			{winrate: 0.5, scoreLead: 0, visits: 10},
		},
	}

	data, err := annotatedSGFForVariation(collection, 9, nil, analysis)
	if err != nil {
		t.Fatalf("annotatedSGFForVariation returned error: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "Original root note") {
		t.Fatalf("annotated SGF lost original root comment: %s", text)
	}
	if !strings.Contains(text, "sgf2gif KataGo summary") {
		t.Fatalf("annotated SGF missing appended diagnostics comment: %s", text)
	}
	if !strings.Contains(text, "KataGo: platform=linux/amd64") {
		t.Fatalf("annotated SGF missing compact appended summary: %s", text)
	}
}

func TestAnnotatedSGFPath(t *testing.T) {
	if got := annotatedSGFPath("/tmp/out.gif"); got != "/tmp/out.katago.sgf" {
		t.Fatalf("annotatedSGFPath = %q", got)
	}
}

func TestCachedAnalysisFromGameAcceptsLegacyPlainTextDiagnostics(t *testing.T) {
	collection, err := sgf.ParseSgf(`(;FF[4]GM[1]SZ[9]KTCACHE[` + katagoCacheVersionV1 + `]KTDIAG[Platform: linux/amd64]
;B[aa]KT[{"winrate":0.6,"scoreLead":2.5,"visits":100}])`)
	if err != nil {
		t.Fatalf("ParseSgf returned error: %v", err)
	}
	game, err := firstGame(collection)
	if err != nil {
		t.Fatalf("firstGame returned error: %v", err)
	}
	loaded, err := cachedAnalysisFromGame(game, 9, nil)
	if err != nil {
		t.Fatalf("cachedAnalysisFromGame returned error: %v", err)
	}
	if loaded == nil {
		t.Fatal("loaded analysis is nil")
	}
	if loaded.diagnostics != "Platform: linux/amd64" {
		t.Fatalf("loaded diagnostics = %q, want %q", loaded.diagnostics, "Platform: linux/amd64")
	}
	if loaded.cacheMeta != nil {
		t.Fatalf("legacy cache should not invent cacheMeta: %#v", loaded.cacheMeta)
	}
}

func TestCachedAnalysisFromGameRejectsUnsupportedCacheVersion(t *testing.T) {
	collection, err := sgf.ParseSgf(`(;FF[4]GM[1]SZ[9]KTCACHE[sgf2gif-katago-v999]
;B[aa]KT[{"winrate":0.6,"scoreLead":2.5,"visits":100}])`)
	if err != nil {
		t.Fatalf("ParseSgf returned error: %v", err)
	}
	game, err := firstGame(collection)
	if err != nil {
		t.Fatalf("firstGame returned error: %v", err)
	}
	_, err = cachedAnalysisFromGame(game, 9, nil)
	if err == nil {
		t.Fatal("cachedAnalysisFromGame returned nil error for unsupported version")
	}
	if !strings.Contains(err.Error(), "unsupported KataGo cache version") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCanReuseCachedAnalysis(t *testing.T) {
	cached := &analysisSeries{
		cacheMeta: &katagoCacheMetadata{
			Backend:   "cpu",
			MaxVisits: 1000,
			TopMoves:  5,
		},
	}
	reuse, reason := canReuseCachedAnalysis(cached, katagoOptions{
		backend:   "auto",
		maxVisits: 500,
		topMoves:  3,
	})
	if !reuse {
		t.Fatalf("expected cache reuse, got false (%s)", reason)
	}
	if !strings.Contains(reason, "cache maxVisits=1000") {
		t.Fatalf("unexpected reuse reason: %q", reason)
	}
}

func TestCanReuseCachedAnalysisRejectsMissingPlayedHitStats(t *testing.T) {
	cached := &analysisSeries{
		cacheMeta: &katagoCacheMetadata{
			Backend:   "cpu",
			MaxVisits: 1000,
			TopMoves:  5,
		},
		frames: []positionAnalysis{
			{playedMove: "A9"},
		},
	}
	reuse, reason := canReuseCachedAnalysis(cached, katagoOptions{
		backend:   "auto",
		maxVisits: 500,
		topMoves:  3,
	})
	if reuse {
		t.Fatalf("expected cache refresh, got reuse (%s)", reason)
	}
	if !strings.Contains(reason, "played-hit") {
		t.Fatalf("unexpected reject reason: %q", reason)
	}

	cached.frames[0].playedHitKnown = true
	reuse, reason = canReuseCachedAnalysis(cached, katagoOptions{
		backend:   "auto",
		maxVisits: 500,
		topMoves:  3,
	})
	if !reuse {
		t.Fatalf("expected cache reuse, got false (%s)", reason)
	}
}

func TestCanReuseCachedAnalysisRejectsWeakerVisits(t *testing.T) {
	cached := &analysisSeries{
		cacheMeta: &katagoCacheMetadata{
			Backend:   "cpu",
			MaxVisits: 100,
			TopMoves:  5,
		},
	}
	reuse, reason := canReuseCachedAnalysis(cached, katagoOptions{
		backend:   "auto",
		maxVisits: 500,
		topMoves:  3,
	})
	if reuse {
		t.Fatalf("expected cache refresh, got reuse (%s)", reason)
	}
	if !strings.Contains(reason, "maxVisits=100") {
		t.Fatalf("unexpected reject reason: %q", reason)
	}
}

func TestCanReuseCachedAnalysisRejectsDifferentBackend(t *testing.T) {
	cached := &analysisSeries{
		cacheMeta: &katagoCacheMetadata{
			Backend:   "cpu",
			MaxVisits: 1000,
			TopMoves:  5,
		},
	}
	reuse, reason := canReuseCachedAnalysis(cached, katagoOptions{
		backend:   "cuda",
		maxVisits: 500,
		topMoves:  3,
	})
	if reuse {
		t.Fatalf("expected cache refresh, got reuse (%s)", reason)
	}
	if !strings.Contains(reason, "backend=cpu") {
		t.Fatalf("unexpected reject reason: %q", reason)
	}
}

func TestDetermineKataGoCacheActionUsesCacheWhenCompatible(t *testing.T) {
	cached := &analysisSeries{
		cacheMeta: &katagoCacheMetadata{
			Backend:   "cpu",
			MaxVisits: 1000,
			TopMoves:  5,
		},
	}
	action, reason := determineKataGoCacheAction(cached, katagoOptions{
		backend:   "auto",
		maxVisits: 500,
		topMoves:  3,
	}, false, false)
	if action != katagoCacheActionUse {
		t.Fatalf("action = %q, want %q (%s)", action, katagoCacheActionUse, reason)
	}
}

func TestDetermineKataGoCacheActionRunsWhenRefreshForced(t *testing.T) {
	action, reason := determineKataGoCacheAction(nil, katagoOptions{}, true, false)
	if action != katagoCacheActionRun {
		t.Fatalf("action = %q, want %q (%s)", action, katagoCacheActionRun, reason)
	}
	if reason != "forced by --katago-refresh" {
		t.Fatalf("reason = %q", reason)
	}
}

func TestDetermineKataGoCacheActionFailsWhenCacheOnlyMisses(t *testing.T) {
	action, reason := determineKataGoCacheAction(nil, katagoOptions{}, false, true)
	if action != katagoCacheActionFail {
		t.Fatalf("action = %q, want %q (%s)", action, katagoCacheActionFail, reason)
	}
	if !strings.Contains(reason, "no cached analysis found") {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestCanReuseCachedAnalysisRejectsUnknownBackendForExplicitRequest(t *testing.T) {
	cached := &analysisSeries{
		cacheMeta: &katagoCacheMetadata{
			Backend:   unknownKataGoBackend,
			MaxVisits: 1000,
			TopMoves:  5,
		},
	}
	reuse, reason := canReuseCachedAnalysis(cached, katagoOptions{
		backend:   "cuda",
		maxVisits: 500,
		topMoves:  3,
	})
	if reuse {
		t.Fatalf("expected cache refresh, got reuse (%s)", reason)
	}
	if !strings.Contains(reason, "backend unknown") {
		t.Fatalf("unexpected reject reason: %q", reason)
	}
}

func TestBuildKataGoCacheMetadataUsesResolvedBackend(t *testing.T) {
	meta := buildKataGoCacheMetadata(katagoOptions{
		maxVisits: 500,
		threads:   2,
		workers:   1,
		topMoves:  3,
	}, "cpu")
	if meta.Backend != "cpu" {
		t.Fatalf("meta.Backend = %q, want %q", meta.Backend, "cpu")
	}
	if meta.GeneratedBy != katagoCacheVersionV2 {
		t.Fatalf("meta.GeneratedBy = %q, want %q", meta.GeneratedBy, katagoCacheVersionV2)
	}
}

func TestParseArgsRefreshEnablesKataGo(t *testing.T) {
	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{
		"sgf2gif",
		"--katago-refresh",
		"in.sgf",
		"out.gif",
	}

	opts, err := parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if !opts.enableKataGo {
		t.Fatal("enableKataGo = false, want true when katago-refresh is set")
	}
	if !opts.katagoRefresh {
		t.Fatal("katagoRefresh = false, want true")
	}
}
