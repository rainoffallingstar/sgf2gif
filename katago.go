package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultKataGoRoot          = "katago"
	kataGoLatestReleaseAPIURL  = "https://api.github.com/repos/lightvector/KataGo/releases/latest"
	kataGoNetworksURL          = "https://katagotraining.org/networks/"
	kataGoConfigRawURLTemplate = "https://raw.githubusercontent.com/lightvector/KataGo/%s/cpp/configs/%s"
)

type katagoOptions struct {
	rootDir      string
	binPath      string
	modelPath    string
	configPath   string
	maxVisits    int
	threads      int
	topMoves     int
	releaseAPI   string
	networksPage string
	httpClient   *http.Client
}

type katagoEnvironment struct {
	binPath    string
	modelPath  string
	configPath string
	releaseTag string
}

type analysisSeries struct {
	frames []positionAnalysis
}

type positionAnalysis struct {
	winrate   float64
	scoreLead float64
	visits    int
	topMoves  []analysisMove
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
		rootDir:      defaultKataGoRoot,
		binPath:      opts.katagoBin,
		modelPath:    opts.katagoModel,
		configPath:   opts.katagoConfig,
		maxVisits:    opts.katagoVisits,
		threads:      opts.katagoThreads,
		topMoves:     opts.katagoTopMoves,
		releaseAPI:   kataGoLatestReleaseAPIURL,
		networksPage: kataGoNetworksURL,
		httpClient:   http.DefaultClient,
	}
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

	queries := buildKataGoQueries(info, specs, opts.maxVisits)
	results, err := runKataGoAnalysis(env, queries, opts)
	if err != nil {
		return nil, err
	}

	frames := make([]positionAnalysis, len(specs))
	for i, query := range queries {
		result, ok := results[query.ID]
		if !ok {
			return nil, fmt.Errorf("missing KataGo response for %s", query.ID)
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
			x, y, pass, err := parseGTPMove(moveInfo.Move, specs[i].state.size)
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

	return &analysisSeries{frames: frames}, nil
}

func buildKataGoQueries(info *gameInfo, specs []frameSpec, maxVisits int) []katagoAnalysisQuery {
	queries := make([]katagoAnalysisQuery, 0, len(specs))
	rules := normalizeKataGoRules(info.rules)
	komi := parseKomiValue(info.komi)
	for i, spec := range specs {
		query := katagoAnalysisQuery{
			ID:            fmt.Sprintf("frame-%04d", i),
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
	}
	return queries
}

func runKataGoAnalysis(env katagoEnvironment, queries []katagoAnalysisQuery, opts katagoOptions) (map[string]katagoAnalysisResponse, error) {
	args := []string{
		"analysis",
		"-config", env.configPath,
		"-model", env.modelPath,
		"-override-config", fmt.Sprintf("numAnalysisThreads=%d", opts.threads),
		"-quit-without-waiting",
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
		binPath:    opts.binPath,
		modelPath:  opts.modelPath,
		configPath: opts.configPath,
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
			asset, err := selectKataGoAsset(release, runtime.GOOS, runtime.GOARCH)
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

func selectKataGoAsset(release *githubRelease, goos, goarch string) (*githubReleaseAsset, error) {
	target := platformAssetNeedle(goos, goarch)
	if target == "" {
		return nil, fmt.Errorf("automatic KataGo download is not supported for %s/%s", goos, goarch)
	}

	preferences := []string{"eigenavx2", "eigen", "opencl"}
	for _, pref := range preferences {
		for _, asset := range release.Assets {
			name := strings.ToLower(asset.Name)
			if !strings.Contains(name, target) || !strings.HasSuffix(name, ".zip") {
				continue
			}
			if strings.Contains(name, pref) && !strings.Contains(name, "cuda") && !strings.Contains(name, "trt") {
				return &asset, nil
			}
		}
	}
	for _, asset := range release.Assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, target) && strings.HasSuffix(name, ".zip") {
			return &asset, nil
		}
	}
	return nil, fmt.Errorf("could not find a KataGo download asset for %s/%s in release %s", goos, goarch, release.TagName)
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

func toGTPMove(x, y, boardSize int) string {
	return fmt.Sprintf("%s%d", columnLabel(x), boardLabelY(y, boardSize))
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
