package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultKataGoRoot          = "katago"
	kataGoLatestReleaseAPIURL  = "https://api.github.com/repos/lightvector/KataGo/releases/latest"
	kataGoNetworksURL          = "https://katagotraining.org/networks/"
	kataGoConfigRawURLTemplate = "https://raw.githubusercontent.com/lightvector/KataGo/%s/cpp/configs/%s"
)

type katagoOptions struct {
	rootDir        string
	binPath        string
	modelPath      string
	configPath     string
	diagnosticsOut string
	backend        string
	maxVisits      int
	threads        int
	workers        int
	topMoves       int
	releaseAPI     string
	networksPage   string
	httpClient     *http.Client
}

type katagoEnvironment struct {
	binPath         string
	modelPath       string
	configPath      string
	releaseTag      string
	resolvedBackend string
}

type analysisSeries struct {
	frames      []positionAnalysis
	summary     *analysisSummary
	diagnostics string
	cacheMeta   *katagoCacheMetadata
}

type positionAnalysis struct {
	winrate       float64
	scoreLead     float64
	visits        int
	topMoves      []analysisMove
	playedMove    string
	bestMove      string
	moveLoss      float64
	lossKnown     bool
	bestWinrate   float64
	actualWinrate float64
	winrateGap    float64
}

type decisionQueryRef struct {
	frameIndex int
	before     *boardState
	move       *move
}

type analysisMove struct {
	move      string
	x         int
	y         int
	pass      bool
	visits    int
	order     int
	winrate   float64
	scoreLead float64
}

type githubRelease struct {
	TagName string               `json:"tag_name"`
	Assets  []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type katagoAnalysisQuery struct {
	ID            string     `json:"id"`
	InitialStones [][]string `json:"initialStones,omitempty"`
	InitialPlayer string     `json:"initialPlayer,omitempty"`
	Rules         string     `json:"rules,omitempty"`
	Komi          *float64   `json:"komi,omitempty"`
	BoardXSize    int        `json:"boardXSize"`
	BoardYSize    int        `json:"boardYSize"`
	Moves         [][]string `json:"moves"`
	AnalyzeTurns  []int      `json:"analyzeTurns,omitempty"`
	MaxVisits     int        `json:"maxVisits,omitempty"`
}

type katagoAnalysisResponse struct {
	ID             string           `json:"id"`
	Error          string           `json:"error"`
	IsDuringSearch bool             `json:"isDuringSearch"`
	RootInfo       katagoRootInfo   `json:"rootInfo"`
	MoveInfos      []katagoMoveInfo `json:"moveInfos"`
}

type katagoRootInfo struct {
	Winrate   float64 `json:"winrate"`
	ScoreLead float64 `json:"scoreLead"`
	Visits    int     `json:"visits"`
}

type katagoMoveInfo struct {
	Move      string   `json:"move"`
	Visits    int      `json:"visits"`
	Order     int      `json:"order"`
	Winrate   float64  `json:"winrate"`
	ScoreLead float64  `json:"scoreLead"`
	PV        []string `json:"pv"`
}

func katagoOptionsFromCLI(opts *options) katagoOptions {
	return katagoOptions{
		rootDir:        defaultKataGoRoot,
		binPath:        opts.katagoBin,
		modelPath:      opts.katagoModel,
		configPath:     opts.katagoConfig,
		diagnosticsOut: opts.katagoDiagnosticsOut,
		backend:        opts.katagoBackend,
		maxVisits:      opts.katagoVisits,
		threads:        opts.katagoThreads,
		workers:        opts.katagoWorkers,
		topMoves:       opts.katagoTopMoves,
		releaseAPI:     kataGoLatestReleaseAPIURL,
		networksPage:   kataGoNetworksURL,
		httpClient:     http.DefaultClient,
	}
}

type katagoWorkerPlan struct {
	queries []katagoAnalysisQuery
	threads int
}

func (s *analysisSeries) frameAt(i int) *positionAnalysis {
	if s == nil || i < 0 || i >= len(s.frames) {
		return nil
	}
	return &s.frames[i]
}

func analyzeActionsWithKataGo(info *gameInfo, initial *boardState, actions []*action, rule koRule, opts katagoOptions) (*analysisSeries, error) {
	specs, err := actionsToFrameSpecs(initial, actions, rule)
	if err != nil {
		return nil, err
	}
	if len(specs) == 0 {
		return nil, nil
	}

	env, err := ensureKataGoEnvironment(opts)
	if err != nil {
		return nil, err
	}

	queries, decisions := buildKataGoQueries(info, specs, opts.maxVisits)
	results, err := runKataGoAnalysis(env, queries, opts)
	if err != nil {
		return nil, err
	}

	frames, err := populateAnalysisFrames(specs, results, opts)
	if err != nil {
		return nil, err
	}
	applyDecisionAnalysis(frames, decisions, results)
	series := &analysisSeries{frames: frames}
	series.diagnostics, _ = buildKataGoDiagnosticsReport(opts)
	series.cacheMeta = buildKataGoCacheMetadata(opts, env.resolvedBackend)
	series.summary = buildAnalysisSummary(actions, series)
	return series, nil
}

func populateAnalysisFrames(specs []frameSpec, results map[string]katagoAnalysisResponse, opts katagoOptions) ([]positionAnalysis, error) {
	frames := make([]positionAnalysis, len(specs))
	for i, spec := range specs {
		result, ok := results[frameQueryID(i)]
		if !ok {
			return nil, fmt.Errorf("missing KataGo response for %s", frameQueryID(i))
		}

		topMoves := make([]analysisMove, 0, len(result.MoveInfos))
		moveInfos := append([]katagoMoveInfo(nil), result.MoveInfos...)
		sort.SliceStable(moveInfos, func(i, j int) bool {
			if moveInfos[i].Order == moveInfos[j].Order {
				return moveInfos[i].Visits > moveInfos[j].Visits
			}
			return moveInfos[i].Order < moveInfos[j].Order
		})
		for _, moveInfo := range moveInfos {
			if opts.topMoves > 0 && len(topMoves) >= opts.topMoves {
				break
			}
			x, y, pass, err := parseGTPMove(moveInfo.Move, spec.state.size)
			if err != nil {
				continue
			}
			topMoves = append(topMoves, analysisMove{
				move:      moveInfo.Move,
				x:         x,
				y:         y,
				pass:      pass,
				visits:    moveInfo.Visits,
				order:     moveInfo.Order,
				winrate:   moveInfo.Winrate,
				scoreLead: moveInfo.ScoreLead,
			})
		}

		frames[i] = positionAnalysis{
			winrate:   result.RootInfo.Winrate,
			scoreLead: result.RootInfo.ScoreLead,
			visits:    result.RootInfo.Visits,
			topMoves:  topMoves,
		}
	}
	return frames, nil
}

func buildKataGoQueries(info *gameInfo, specs []frameSpec, maxVisits int) ([]katagoAnalysisQuery, []decisionQueryRef) {
	queries := make([]katagoAnalysisQuery, 0, len(specs))
	decisions := make([]decisionQueryRef, 0, len(specs))
	rules := normalizeKataGoRules(info.rules)
	komi := parseKomiValue(info.komi)
	for i, spec := range specs {
		query := katagoAnalysisQuery{
			ID:            frameQueryID(i),
			InitialStones: spec.state.kataGoInitialStones(),
			InitialPlayer: playerColorCode(spec.state.toPlay),
			Rules:         rules,
			Komi:          komi,
			BoardXSize:    spec.state.size,
			BoardYSize:    spec.state.size,
			Moves:         [][]string{},
			AnalyzeTurns:  []int{0},
			MaxVisits:     maxVisits,
		}
		queries = append(queries, query)

		if spec.beforeMoveState != nil && spec.current != nil && spec.current.move != nil {
			queries = append(queries, katagoAnalysisQuery{
				ID:            fmt.Sprintf("decision-%04d", i),
				InitialStones: spec.beforeMoveState.kataGoInitialStones(),
				InitialPlayer: playerColorCode(spec.beforeMoveState.toPlay),
				Rules:         rules,
				Komi:          komi,
				BoardXSize:    spec.beforeMoveState.size,
				BoardYSize:    spec.beforeMoveState.size,
				Moves:         [][]string{},
				AnalyzeTurns:  []int{0},
				MaxVisits:     maxVisits,
			})
			decisions = append(decisions, decisionQueryRef{
				frameIndex: i,
				before:     spec.beforeMoveState,
				move:       spec.current.move,
			})
		}
	}
	return queries, decisions
}

func runKataGoAnalysis(env katagoEnvironment, queries []katagoAnalysisQuery, opts katagoOptions) (map[string]katagoAnalysisResponse, error) {
	if len(queries) == 0 {
		return map[string]katagoAnalysisResponse{}, nil
	}

	startedAt := time.Now()
	plans := buildKataGoWorkerPlans(queries, opts.workers, opts.threads)
	if len(plans) == 1 {
		total := len(queries)
		completed := 0
		printAnalysisProgress(completed, total, startedAt)
		responses, err := runSingleKataGoAnalysis(env, plans[0].queries, plans[0].threads, func() {
			completed++
			printAnalysisProgress(completed, total, startedAt)
		})
		if total > 0 {
			fmt.Fprintln(os.Stdout)
			fmt.Fprintf(os.Stdout, "KataGo analysis finished in %s\n", formatKataGoDuration(time.Since(startedAt)))
		}
		return responses, err
	}

	total := len(queries)
	completed := 0
	printAnalysisProgress(completed, total, startedAt)
	progressCh := make(chan struct{}, total)
	progressDone := make(chan struct{})
	go func() {
		for range progressCh {
			completed++
			printAnalysisProgress(completed, total, startedAt)
		}
		close(progressDone)
	}()

	type workerResult struct {
		responses map[string]katagoAnalysisResponse
		err       error
	}

	resultsCh := make(chan workerResult, len(plans))
	var wg sync.WaitGroup
	for _, plan := range plans {
		plan := plan
		wg.Add(1)
		go func() {
			defer wg.Done()
			responses, err := runSingleKataGoAnalysis(env, plan.queries, plan.threads, func() {
				progressCh <- struct{}{}
			})
			resultsCh <- workerResult{responses: responses, err: err}
		}()
	}
	go func() {
		wg.Wait()
		close(progressCh)
		close(resultsCh)
	}()

	merged := make(map[string]katagoAnalysisResponse, len(queries))
	var firstErr error
	for result := range resultsCh {
		if result.err != nil && firstErr == nil {
			firstErr = result.err
			continue
		}
		for id, response := range result.responses {
			merged[id] = response
		}
	}
	<-progressDone
	if total > 0 {
		fmt.Fprintln(os.Stdout)
		fmt.Fprintf(os.Stdout, "KataGo analysis finished in %s\n", formatKataGoDuration(time.Since(startedAt)))
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return merged, nil
}

func runSingleKataGoAnalysis(env katagoEnvironment, queries []katagoAnalysisQuery, threads int, onComplete func()) (map[string]katagoAnalysisResponse, error) {
	args := []string{
		"analysis",
		"-config", env.configPath,
		"-model", env.modelPath,
		"-override-config", fmt.Sprintf("numAnalysisThreads=%d", threads),
	}
	cmd := exec.Command(env.binPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	var stderrBuf bytes.Buffer
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	errCh := make(chan error, 2)
	go func() {
		_, readErr := io.Copy(&stderrBuf, stderr)
		errCh <- readErr
	}()

	responses := map[string]katagoAnalysisResponse{}
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var resp katagoAnalysisResponse
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				errCh <- fmt.Errorf("failed to decode KataGo response: %w", err)
				return
			}
			if resp.IsDuringSearch {
				continue
			}
			if resp.Error != "" {
				errCh <- fmt.Errorf("katago analysis error for %s: %s", resp.ID, resp.Error)
				return
			}
			responses[resp.ID] = resp
			if onComplete != nil {
				onComplete()
			}
		}
		errCh <- scanner.Err()
	}()

	encoder := json.NewEncoder(stdin)
	for _, query := range queries {
		if err := encoder.Encode(query); err != nil {
			_ = stdin.Close()
			_ = cmd.Wait()
			return nil, err
		}
	}
	if err := stdin.Close(); err != nil {
		_ = cmd.Wait()
		return nil, err
	}

	var firstErr error
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil && firstErr == nil {
			firstErr = err
		}
	}

	waitErr := cmd.Wait()
	if firstErr != nil {
		return nil, fmt.Errorf("%w\n%s", firstErr, strings.TrimSpace(stderrBuf.String()))
	}
	if waitErr != nil {
		return nil, fmt.Errorf("katago exited with error: %w\n%s", waitErr, strings.TrimSpace(stderrBuf.String()))
	}
	if len(responses) == 0 {
		message := strings.TrimSpace(stderrBuf.String())
		if message == "" {
			message = "KataGo returned no analysis responses"
		}
		return nil, errors.New(message)
	}

	return responses, nil
}

func buildKataGoWorkerPlans(queries []katagoAnalysisQuery, workers, totalThreads int) []katagoWorkerPlan {
	if len(queries) == 0 {
		return nil
	}
	if workers <= 0 {
		workers = 1
	}
	if totalThreads <= 0 {
		totalThreads = 1
	}
	if workers > len(queries) {
		workers = len(queries)
	}
	if workers > totalThreads {
		workers = totalThreads
	}
	if workers <= 1 {
		return []katagoWorkerPlan{{
			queries: queries,
			threads: totalThreads,
		}}
	}

	plans := make([]katagoWorkerPlan, 0, workers)
	baseQueries := len(queries) / workers
	extraQueries := len(queries) % workers
	baseThreads := totalThreads / workers
	extraThreads := totalThreads % workers
	start := 0
	for i := 0; i < workers; i++ {
		queryCount := baseQueries
		if i < extraQueries {
			queryCount++
		}
		threadCount := baseThreads
		if i < extraThreads {
			threadCount++
		}
		if threadCount < 1 {
			threadCount = 1
		}
		end := start + queryCount
		plans = append(plans, katagoWorkerPlan{
			queries: queries[start:end],
			threads: threadCount,
		})
		start = end
	}
	return plans
}

func printAnalysisProgress(done, total int, startedAt time.Time) {
	if total <= 0 {
		return
	}
	pct := 100 * float64(done) / float64(total)
	const barWidth = 24
	filled := int(math.Round(float64(done) / float64(total) * barWidth))
	if filled < 0 {
		filled = 0
	}
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("#", filled) + strings.Repeat("-", barWidth-filled)
	elapsed := time.Since(startedAt)
	eta := "--:--"
	if done > 0 && done < total {
		remaining := time.Duration(float64(elapsed) * float64(total-done) / float64(done))
		eta = formatKataGoDuration(remaining)
	} else if done >= total {
		eta = "00:00"
	}
	fmt.Fprintf(
		os.Stdout,
		"\rKataGo analysis progress: [%s] %d/%d (%.1f%%) elapsed %s eta %s",
		bar,
		done,
		total,
		pct,
		formatKataGoDuration(elapsed),
		eta,
	)
}

func formatKataGoDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	seconds := int(d.Round(time.Second) / time.Second)
	minutes := seconds / 60
	seconds = seconds % 60
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func ensureKataGoEnvironment(opts katagoOptions) (katagoEnvironment, error) {
	if opts.rootDir == "" {
		opts.rootDir = defaultKataGoRoot
	}
	if opts.httpClient == nil {
		opts.httpClient = http.DefaultClient
	}
	if opts.releaseAPI == "" {
		opts.releaseAPI = kataGoLatestReleaseAPIURL
	}
	if opts.networksPage == "" {
		opts.networksPage = kataGoNetworksURL
	}

	rootDir := opts.rootDir
	env := katagoEnvironment{
		binPath:         opts.binPath,
		modelPath:       opts.modelPath,
		configPath:      opts.configPath,
		resolvedBackend: unknownKataGoBackend,
	}
	if env.binPath == "" {
		env.binPath = filepath.Join(rootDir, "bin", kataGoExecutableName())
	}
	if env.modelPath == "" {
		env.modelPath = filepath.Join(rootDir, "models", "default_model.bin.gz")
	}
	if env.configPath == "" {
		env.configPath = filepath.Join(rootDir, "configs", "analysis_example.cfg")
	}

	if fileExists(env.binPath) {
		tag, _ := detectKataGoVersionTag(env.binPath)
		env.releaseTag = tag
		if backend := readKataGoBackendMarker(rootDir); backend != "" {
			env.resolvedBackend = backend
		}
	} else if opts.binPath != "" {
		return env, fmt.Errorf("KataGo binary not found: %s", env.binPath)
	} else if path, err := exec.LookPath("katago"); err == nil {
		env.binPath = path
		tag, _ := detectKataGoVersionTag(env.binPath)
		env.releaseTag = tag
	} else {
		switch runtime.GOOS {
		case "darwin":
			return env, fmt.Errorf("KataGo is not installed. On macOS, install it with `brew install katago`")
		case "linux", "windows":
			release, err := fetchLatestKataGoRelease(opts.httpClient, opts.releaseAPI)
			if err != nil {
				return env, err
			}
			backends, _, err := preferredKataGoBackends(runtime.GOOS, opts.backend)
			if err != nil {
				return env, err
			}
			asset, resolvedBackend, err := selectKataGoAsset(release, runtime.GOOS, runtime.GOARCH, backends)
			if err != nil {
				return env, err
			}
			if err := os.MkdirAll(filepath.Dir(env.binPath), 0o755); err != nil {
				return env, err
			}
			if err := downloadAndExtractKataGoBinary(opts.httpClient, asset.URL, env.binPath); err != nil {
				return env, err
			}
			env.releaseTag = release.TagName
			env.resolvedBackend = resolvedBackend
			if err := writeKataGoBackendMarker(rootDir, resolvedBackend); err != nil {
				return env, err
			}
		default:
			return env, fmt.Errorf("automatic KataGo download is not supported on %s", runtime.GOOS)
		}
	}

	if !fileExists(env.modelPath) {
		if opts.modelPath != "" {
			return env, fmt.Errorf("KataGo model not found: %s", env.modelPath)
		}
		if err := os.MkdirAll(filepath.Dir(env.modelPath), 0o755); err != nil {
			return env, err
		}
		modelURL, err := fetchLatestModelURL(opts.httpClient, opts.networksPage)
		if err != nil {
			return env, err
		}
		if err := downloadFile(opts.httpClient, modelURL, env.modelPath, 0o644); err != nil {
			return env, err
		}
	}

	if !fileExists(env.configPath) {
		if opts.configPath != "" {
			return env, fmt.Errorf("KataGo config not found: %s", env.configPath)
		}
		if err := os.MkdirAll(filepath.Dir(env.configPath), 0o755); err != nil {
			return env, err
		}
		if env.releaseTag == "" {
			release, err := fetchLatestKataGoRelease(opts.httpClient, opts.releaseAPI)
			if err != nil {
				return env, err
			}
			env.releaseTag = release.TagName
		}
		configURL := fmt.Sprintf(kataGoConfigRawURLTemplate, env.releaseTag, "analysis_example.cfg")
		if err := downloadFile(opts.httpClient, configURL, env.configPath, 0o644); err != nil {
			return env, err
		}
	}

	return env, nil
}

func detectKataGoSetup(opts katagoOptions) error {
	text, err := buildKataGoDiagnosticsReport(opts)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(os.Stdout, text); err != nil {
		return err
	}
	if opts.diagnosticsOut != "" {
		if err := saveBytes(opts.diagnosticsOut, []byte(text)); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "KataGo diagnostics saved to %s\n", opts.diagnosticsOut)
	}
	return nil
}

func buildKataGoDiagnosticsReport(opts katagoOptions) (string, error) {
	if opts.rootDir == "" {
		opts.rootDir = defaultKataGoRoot
	}
	if opts.httpClient == nil {
		opts.httpClient = http.DefaultClient
	}
	if opts.releaseAPI == "" {
		opts.releaseAPI = kataGoLatestReleaseAPIURL
	}
	if opts.networksPage == "" {
		opts.networksPage = kataGoNetworksURL
	}

	env := katagoEnvironment{
		binPath:    opts.binPath,
		modelPath:  opts.modelPath,
		configPath: opts.configPath,
	}
	if env.binPath == "" {
		env.binPath = filepath.Join(opts.rootDir, "bin", kataGoExecutableName())
	}
	if env.modelPath == "" {
		env.modelPath = filepath.Join(opts.rootDir, "models", "default_model.bin.gz")
	}
	if env.configPath == "" {
		env.configPath = filepath.Join(opts.rootDir, "configs", "analysis_example.cfg")
	}

	var report strings.Builder
	fmt.Fprintln(&report, "KataGo detect-only mode")
	fmt.Fprintf(&report, "Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)

	backends, reason, err := preferredKataGoBackends(runtime.GOOS, opts.backend)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&report, "KataGo backend preference: %s -> %s (%s)\n", normalizeBackendLabel(opts.backend), strings.Join(backends, " -> "), reason)

	releaseTag := ""
	if fileExists(env.binPath) {
		releaseTag, _ = detectKataGoVersionTag(env.binPath)
		fmt.Fprintf(&report, "KataGo binary: existing file at %s", env.binPath)
		if releaseTag != "" {
			fmt.Fprintf(&report, " (%s)", releaseTag)
		}
		fmt.Fprintln(&report)
	} else if opts.binPath != "" {
		return "", fmt.Errorf("KataGo binary not found: %s", env.binPath)
	} else if pathOnPATH, err := exec.LookPath("katago"); err == nil {
		env.binPath = pathOnPATH
		releaseTag, _ = detectKataGoVersionTag(env.binPath)
		fmt.Fprintf(&report, "KataGo binary: found on PATH at %s", env.binPath)
		if releaseTag != "" {
			fmt.Fprintf(&report, " (%s)", releaseTag)
		}
		fmt.Fprintln(&report)
	} else {
		switch runtime.GOOS {
		case "darwin":
			fmt.Fprintln(&report, "KataGo binary: not installed; automatic download is disabled on macOS")
			fmt.Fprintln(&report, "Install hint: brew install katago")
		case "linux", "windows":
			release, err := fetchLatestKataGoRelease(opts.httpClient, opts.releaseAPI)
			if err != nil {
				return "", err
			}
			releaseTag = release.TagName
			asset, resolvedBackend, err := selectKataGoAsset(release, runtime.GOOS, runtime.GOARCH, backends)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&report, "KataGo binary: would download %s using backend %s\n", asset.Name, resolvedBackend)
			fmt.Fprintf(&report, "KataGo binary target: %s\n", env.binPath)
			if resolvedBackend != backends[0] {
				fmt.Fprintf(&report, "KataGo backend fallback: requested %s but would select %s because no matching %s asset was available for %s/%s\n", backends[0], resolvedBackend, backends[0], runtime.GOOS, runtime.GOARCH)
			}
		default:
			fmt.Fprintf(&report, "KataGo binary: automatic download is not supported on %s\n", runtime.GOOS)
		}
	}

	switch {
	case fileExists(env.modelPath):
		fmt.Fprintf(&report, "KataGo model: existing file at %s\n", env.modelPath)
	case opts.modelPath != "":
		return "", fmt.Errorf("KataGo model not found: %s", env.modelPath)
	default:
		modelURL, err := fetchLatestModelURL(opts.httpClient, opts.networksPage)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&report, "KataGo model: would download %s\n", modelURL)
		fmt.Fprintf(&report, "KataGo model target: %s\n", env.modelPath)
	}

	switch {
	case fileExists(env.configPath):
		fmt.Fprintf(&report, "KataGo config: existing file at %s\n", env.configPath)
	case opts.configPath != "":
		return "", fmt.Errorf("KataGo config not found: %s", env.configPath)
	default:
		if releaseTag == "" {
			release, err := fetchLatestKataGoRelease(opts.httpClient, opts.releaseAPI)
			if err != nil {
				return "", err
			}
			releaseTag = release.TagName
		}
		configURL := fmt.Sprintf(kataGoConfigRawURLTemplate, releaseTag, "analysis_example.cfg")
		fmt.Fprintf(&report, "KataGo config: would download %s\n", configURL)
		fmt.Fprintf(&report, "KataGo config target: %s\n", env.configPath)
	}

	return report.String(), nil
}

func fetchLatestKataGoRelease(client *http.Client, url string) (*githubRelease, error) {
	body, err := downloadBytes(client, url)
	if err != nil {
		return nil, err
	}
	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("failed to parse latest KataGo release metadata: %w", err)
	}
	if release.TagName == "" {
		return nil, fmt.Errorf("latest KataGo release metadata did not include a tag")
	}
	return &release, nil
}

func selectKataGoAsset(release *githubRelease, goos, goarch string, backends []string) (*githubReleaseAsset, string, error) {
	target := platformAssetNeedle(goos, goarch)
	if target == "" {
		return nil, "", fmt.Errorf("automatic KataGo download is not supported for %s/%s", goos, goarch)
	}
	for _, candidate := range backends {
		if asset := findKataGoAssetForBackend(release.Assets, target, candidate); asset != nil {
			return asset, candidate, nil
		}
	}
	return nil, "", fmt.Errorf(
		"could not find a KataGo download asset for %s/%s with backend %s in release %s",
		goos,
		goarch,
		strings.Join(backends, ","),
		release.TagName,
	)
}

func preferredKataGoBackends(goos, backend string) ([]string, string, error) {
	switch backend {
	case "", "auto":
		nvidiaSignals := detectNVIDIABackendSignals(goos)
		if len(nvidiaSignals) > 0 {
			return []string{"cuda", "opencl", "cpu"}, fmt.Sprintf("detected NVIDIA/CUDA runtime via %s", strings.Join(nvidiaSignals, ", ")), nil
		}
		openclSignals := detectOpenCLBackendSignals(goos)
		if len(openclSignals) > 0 {
			return []string{"opencl", "cpu"}, fmt.Sprintf("detected OpenCL runtime via %s", strings.Join(openclSignals, ", ")), nil
		}
		return []string{"cpu", "opencl"}, "no GPU runtime detected", nil
	case "cpu":
		return []string{"cpu"}, "explicit CPU backend requested", nil
	case "opencl":
		return []string{"opencl", "cpu"}, "explicit OpenCL backend requested", nil
	case "cuda":
		return []string{"cuda", "opencl", "cpu"}, "explicit CUDA backend requested", nil
	default:
		return nil, "", fmt.Errorf("katago-backend must be one of: auto, cpu, opencl, cuda")
	}
}

func normalizeBackendLabel(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "auto"
	}
	return value
}

const unknownKataGoBackend = "unknown"

func normalizeStoredBackendLabel(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func kataGoBackendMarkerPath(rootDir string) string {
	return filepath.Join(rootDir, "bin", "backend.txt")
}

func readKataGoBackendMarker(rootDir string) string {
	if strings.TrimSpace(rootDir) == "" {
		return ""
	}
	data, err := os.ReadFile(kataGoBackendMarkerPath(rootDir))
	if err != nil {
		return ""
	}
	backend := normalizeStoredBackendLabel(string(data))
	switch backend {
	case "cpu", "opencl", "cuda":
		return backend
	default:
		return ""
	}
}

func writeKataGoBackendMarker(rootDir, backend string) error {
	backend = normalizeStoredBackendLabel(backend)
	switch backend {
	case "cpu", "opencl", "cuda":
		return saveBytes(kataGoBackendMarkerPath(rootDir), []byte(backend+"\n"))
	default:
		return nil
	}
}

func findKataGoAssetForBackend(assets []githubReleaseAsset, target, backend string) *githubReleaseAsset {
	for _, preference := range kataGoBackendAssetPreferences(backend) {
		for i := range assets {
			name := strings.ToLower(assets[i].Name)
			if !strings.Contains(name, target) || !strings.HasSuffix(name, ".zip") {
				continue
			}
			if kataGoAssetMatchesPreference(name, preference) {
				return &assets[i]
			}
		}
	}
	return nil
}

func kataGoBackendAssetPreferences(backend string) []string {
	switch backend {
	case "cuda":
		return []string{"cuda", "trt"}
	case "opencl":
		return []string{"opencl"}
	default:
		return []string{"eigenavx2", "eigen"}
	}
}

func kataGoAssetMatchesPreference(name, preference string) bool {
	switch preference {
	case "eigenavx2":
		return strings.Contains(name, "eigenavx2")
	case "eigen":
		return strings.Contains(name, "eigen") &&
			!strings.Contains(name, "opencl") &&
			!strings.Contains(name, "cuda") &&
			!strings.Contains(name, "trt")
	case "opencl":
		return strings.Contains(name, "opencl")
	case "cuda":
		return strings.Contains(name, "cuda")
	case "trt":
		return strings.Contains(name, "trt") || strings.Contains(name, "tensorrt")
	default:
		return false
	}
}

func detectNVIDIABackendSignals(goos string) []string {
	signals := make([]string, 0, 4)
	if path, err := exec.LookPath("nvidia-smi"); err == nil && path != "" {
		signals = append(signals, "nvidia-smi")
	}
	if value := strings.TrimSpace(os.Getenv("CUDA_PATH")); value != "" {
		signals = append(signals, "CUDA_PATH")
	}
	if value := strings.TrimSpace(os.Getenv("CUDA_HOME")); value != "" {
		signals = append(signals, "CUDA_HOME")
	}
	if value := strings.TrimSpace(os.Getenv("NVIDIA_VISIBLE_DEVICES")); value != "" && strings.ToLower(value) != "void" {
		signals = append(signals, "NVIDIA_VISIBLE_DEVICES")
	}
	switch goos {
	case "linux":
		if pathExists("/proc/driver/nvidia/version") {
			signals = append(signals, "/proc/driver/nvidia/version")
		}
	case "windows":
		programFiles := strings.TrimSpace(os.Getenv("ProgramFiles"))
		if programFiles != "" && pathExists(filepath.Join(programFiles, "NVIDIA Corporation", "NVSMI", "nvidia-smi.exe")) {
			signals = append(signals, "NVSMI/nvidia-smi.exe")
		}
	}
	return dedupeStrings(signals)
}

func detectOpenCLBackendSignals(goos string) []string {
	signals := make([]string, 0, 4)
	if path, err := exec.LookPath("clinfo"); err == nil && path != "" {
		signals = append(signals, "clinfo")
	}
	switch goos {
	case "linux":
		for _, candidate := range []string{
			"/etc/OpenCL/vendors",
			"/usr/lib/libOpenCL.so",
			"/usr/lib64/libOpenCL.so",
			"/usr/lib/x86_64-linux-gnu/libOpenCL.so",
			"/usr/lib/aarch64-linux-gnu/libOpenCL.so",
		} {
			if pathExists(candidate) {
				signals = append(signals, candidate)
			}
		}
	case "windows":
		systemRoot := strings.TrimSpace(os.Getenv("SystemRoot"))
		if systemRoot != "" && pathExists(filepath.Join(systemRoot, "System32", "OpenCL.dll")) {
			signals = append(signals, "System32/OpenCL.dll")
		}
	}
	return dedupeStrings(signals)
}

func platformAssetNeedle(goos, goarch string) string {
	switch goos {
	case "windows":
		if goarch == "amd64" {
			return "windows-x64"
		}
	case "linux":
		switch goarch {
		case "amd64":
			return "linux-x64"
		case "arm64":
			return "linux-arm64"
		}
	}
	return ""
}

func fetchLatestModelURL(client *http.Client, pageURL string) (string, error) {
	body, err := downloadBytes(client, pageURL)
	if err != nil {
		return "", err
	}
	urls := extractModelURLs(string(body))
	if len(urls) == 0 {
		return "", fmt.Errorf("could not find any KataGo model downloads on %s", pageURL)
	}
	for _, url := range urls {
		if strings.Contains(url, "b18c384nbt") {
			return url, nil
		}
	}
	return urls[0], nil
}

func extractModelURLs(html string) []string {
	re := regexp.MustCompile(`https://media\.katagotraining\.org/uploaded/networks/models/[^"' ]+\.bin\.gz`)
	matches := re.FindAllString(html, -1)
	seen := map[string]bool{}
	ret := make([]string, 0, len(matches))
	for _, match := range matches {
		if !seen[match] {
			seen[match] = true
			ret = append(ret, match)
		}
	}
	return ret
}

func downloadAndExtractKataGoBinary(client *http.Client, url, destPath string) error {
	body, err := downloadBytes(client, url)
	if err != nil {
		return err
	}
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return fmt.Errorf("failed to open KataGo archive: %w", err)
	}
	exeName := kataGoExecutableName()
	for _, file := range reader.File {
		if filepath.Base(file.Name) != exeName {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		defer rc.Close()

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("did not find %s in KataGo archive", exeName)
}

func downloadFile(client *http.Client, url, destPath string, mode os.FileMode) error {
	body, err := downloadBytes(client, url)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destPath, body, mode)
}

func downloadBytes(client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "sgf2gif-katago")
	if token := strings.TrimSpace(os.Getenv("GITHUB_PAT")); token != "" && strings.Contains(url, "api.github.com") {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" && strings.Contains(url, "api.github.com") {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("download failed for %s: %s %s", url, resp.Status, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

func detectKataGoVersionTag(binPath string) (string, error) {
	out, err := exec.Command(binPath, "version").CombinedOutput()
	if err != nil {
		return "", err
	}
	re := regexp.MustCompile(`KataGo (v[0-9]+\.[0-9]+\.[0-9]+)`)
	match := re.FindStringSubmatch(string(out))
	if len(match) != 2 {
		return "", fmt.Errorf("failed to parse KataGo version from %q", strings.TrimSpace(string(out)))
	}
	return match[1], nil
}

func normalizeKataGoRules(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	switch {
	case value == "":
		return ""
	case strings.Contains(value, "japanese"):
		return "japanese"
	case strings.Contains(value, "korean"):
		return "korean"
	case strings.Contains(value, "chinese"):
		return "chinese"
	case strings.Contains(value, "aga"):
		return "aga"
	case strings.Contains(value, "new zealand"):
		return "new zealand"
	case strings.Contains(value, "tromp"), strings.Contains(value, "area"):
		return "tromp-taylor"
	default:
		return ""
	}
}

func parseKomiValue(value string) *float64 {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	komi, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return nil
	}
	return &komi
}

func playerColorCode(stone uint8) string {
	if stone == white {
		return "W"
	}
	return "B"
}

func (b *boardState) kataGoInitialStones() [][]string {
	ret := make([][]string, 0, b.size*b.size)
	for y := 0; y < b.size; y++ {
		for x := 0; x < b.size; x++ {
			switch b.get(x, y) {
			case black:
				ret = append(ret, []string{"B", toGTPMove(x, y, b.size)})
			case white:
				ret = append(ret, []string{"W", toGTPMove(x, y, b.size)})
			}
		}
	}
	return ret
}

func moveToGTP(m *move, boardSize int) string {
	if m == nil || m.pass {
		return "PASS"
	}
	return toGTPMove(m.x, m.y, boardSize)
}

func toGTPMove(x, y, boardSize int) string {
	return fmt.Sprintf("%s%d", columnLabel(x), boardLabelY(y, boardSize))
}

func moveInfoByMove(moveInfos []katagoMoveInfo, move string) (katagoMoveInfo, bool) {
	move = strings.ToUpper(strings.TrimSpace(move))
	for _, moveInfo := range moveInfos {
		if strings.ToUpper(strings.TrimSpace(moveInfo.Move)) == move {
			return moveInfo, true
		}
	}
	return katagoMoveInfo{}, false
}

func bestMoveByPlayer(moveInfos []katagoMoveInfo, toPlay uint8) katagoMoveInfo {
	if len(moveInfos) == 0 {
		return katagoMoveInfo{}
	}
	best := moveInfos[0]
	bestScore := moveScoreForPlayer(best, toPlay)
	for _, moveInfo := range moveInfos[1:] {
		score := moveScoreForPlayer(moveInfo, toPlay)
		if score > bestScore || (score == bestScore && moveInfo.Visits > best.Visits) {
			best = moveInfo
			bestScore = score
		}
	}
	return best
}

func moveScoreForPlayer(moveInfo katagoMoveInfo, toPlay uint8) float64 {
	if toPlay == white {
		return -moveInfo.ScoreLead
	}
	return moveInfo.ScoreLead
}

func winrateForPlayer(blackWinrate float64, player uint8) float64 {
	if player == white {
		return 1 - blackWinrate
	}
	return blackWinrate
}

func rootScoreForPlayer(scoreLead float64, toPlay uint8) float64 {
	if toPlay == white {
		return -scoreLead
	}
	return scoreLead
}

func applyDecisionAnalysis(frames []positionAnalysis, decisions []decisionQueryRef, results map[string]katagoAnalysisResponse) {
	for _, decision := range decisions {
		resp, ok := results[decision.id()]
		if !ok || decision.frameIndex < 0 || decision.frameIndex >= len(frames) || decision.move == nil {
			continue
		}
		played := moveToGTP(decision.move, decision.before.size)
		best := bestMoveByPlayer(resp.MoveInfos, decision.before.toPlay)
		frames[decision.frameIndex].playedMove = played
		frames[decision.frameIndex].bestMove = best.Move
		if best.Move == "" {
			continue
		}

		bestLead := moveScoreForPlayer(best, decision.before.toPlay)
		actualLead := rootScoreForPlayer(frames[decision.frameIndex].scoreLead, decision.before.toPlay)
		bestWinrate := winrateForPlayer(best.Winrate, decision.before.toPlay)
		actualWinrate := winrateForPlayer(frames[decision.frameIndex].winrate, decision.before.toPlay)
		frames[decision.frameIndex].moveLoss = bestLead - actualLead
		frames[decision.frameIndex].bestWinrate = bestWinrate
		frames[decision.frameIndex].actualWinrate = actualWinrate
		frames[decision.frameIndex].winrateGap = bestWinrate - actualWinrate
		frames[decision.frameIndex].lossKnown = true
	}
}

func (d decisionQueryRef) id() string {
	return fmt.Sprintf("decision-%04d", d.frameIndex)
}

func frameQueryID(frameIndex int) string {
	return fmt.Sprintf("frame-%04d", frameIndex)
}

func parseGTPMove(value string, boardSize int) (int, int, bool, error) {
	value = strings.TrimSpace(strings.ToUpper(value))
	if value == "" {
		return 0, 0, false, fmt.Errorf("empty KataGo move")
	}
	if value == "PASS" {
		return 0, 0, true, nil
	}
	col := value[0]
	rowText := value[1:]
	if rowText == "" {
		return 0, 0, false, fmt.Errorf("malformed KataGo move: %s", value)
	}
	x, err := parseGTPColumn(col)
	if err != nil {
		return 0, 0, false, err
	}
	row, err := strconv.Atoi(rowText)
	if err != nil {
		return 0, 0, false, fmt.Errorf("malformed KataGo move: %s", value)
	}
	y := boardSize - row
	if x < 0 || y < 0 || x >= boardSize || y >= boardSize {
		return 0, 0, false, fmt.Errorf("KataGo move out of range: %s", value)
	}
	return x, y, false, nil
}

func parseGTPColumn(col byte) (int, error) {
	if col < 'A' || col > 'Z' || col == 'I' {
		return 0, fmt.Errorf("invalid GTP column: %c", col)
	}
	x := int(col - 'A')
	if col > 'I' {
		x--
	}
	return x, nil
}

func kataGoExecutableName() string {
	if runtime.GOOS == "windows" {
		return "katago.exe"
	}
	return "katago"
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func dedupeStrings(values []string) []string {
	if len(values) <= 1 {
		return values
	}
	seen := make(map[string]bool, len(values))
	ret := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		ret = append(ret, value)
	}
	return ret
}
