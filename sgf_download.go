package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	defaultDownloadUserAgent = "sgf2gif/1.0"
	defaultOGSBaseURL        = "https://online-go.com"
	defaultFoxBaseURL        = "https://www.foxwq.com"
)

type remoteInputKind int

const (
	remoteInputUnknown remoteInputKind = iota
	remoteInputOGSGame
	remoteInputOGSUser
	remoteInputFoxGame
	remoteInputDirectSGF
)

type remoteInputSpec struct {
	kind     remoteInputKind
	raw      string
	url      string
	gameID   string
	user     string
	provider string
}

type sgfDownloadResult struct {
	filename string
	data     []byte
}

type sgfDownloadOutput struct {
	path string
	data []byte
}

type sgfDownloadSettings struct {
	client     *http.Client
	cookie     string
	limit      int
	ogsBaseURL string
	foxBaseURL string
}

type ogsPlayerLookupResponse struct {
	Count   int `json:"count"`
	Results []struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
	} `json:"results"`
}

type ogsPlayerGamesResponse struct {
	Next    string `json:"next"`
	Results []struct {
		ID int `json:"id"`
	} `json:"results"`
}

var (
	ogsGameURLPattern = regexp.MustCompile(`/game/([0-9]+)`)
	ogsAPIURLPattern  = regexp.MustCompile(`/api/v1/games/([0-9]+)(/sgf)?/?$`)
	foxGameURLPattern = regexp.MustCompile(`/qipu/newlist/id/([^/.]+)\.html`)
	foxTitlePattern   = regexp.MustCompile(`(?is)<title>(.*?)</title>`)
)

func downloadSGFs(opts *options) ([]sgfDownloadOutput, error) {
	spec, recognized, err := parseRemoteInputSpec(opts.inputPath)
	if err != nil {
		return nil, err
	}
	if !recognized {
		return nil, fmt.Errorf("download-sgf requires a remote source such as ogs:GAME_ID, ogs-user:USERNAME, an OGS URL, or a Fox qipu URL")
	}

	results, err := fetchRemoteSGFs(spec, sgfDownloadSettingsFromOptions(opts))
	if err != nil {
		return nil, err
	}
	return planDownloadOutputs(opts.outputPath, results)
}

func resolveInputPathForParsing(opts *options) (string, func(), error) {
	if _, err := os.Stat(opts.inputPath); err == nil {
		return opts.inputPath, func() {}, nil
	}

	spec, recognized, err := parseRemoteInputSpec(opts.inputPath)
	if err != nil {
		return "", nil, err
	}
	if !recognized {
		return opts.inputPath, func() {}, nil
	}
	if spec.kind == remoteInputOGSUser {
		return "", nil, fmt.Errorf("batch source %q requires --download-sgf", opts.inputPath)
	}

	results, err := fetchRemoteSGFs(spec, sgfDownloadSettingsFromOptions(opts))
	if err != nil {
		return "", nil, err
	}
	if len(results) != 1 {
		return "", nil, fmt.Errorf("expected one remote SGF, got %d", len(results))
	}

	pattern := sanitizeFilename(strings.TrimSuffix(results[0].filename, ".sgf")) + "-*.sgf"
	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", nil, err
	}
	if _, err := tmp.Write(results[0].data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", nil, err
	}

	cleanup := func() {
		_ = os.Remove(tmp.Name())
	}
	return tmp.Name(), cleanup, nil
}

func sgfDownloadSettingsFromOptions(opts *options) sgfDownloadSettings {
	return sgfDownloadSettings{
		client:     http.DefaultClient,
		cookie:     opts.downloadCookie,
		limit:      opts.downloadLimit,
		ogsBaseURL: defaultOGSBaseURL,
		foxBaseURL: defaultFoxBaseURL,
	}
}

func loadDownloadCookie(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return strings.TrimSpace(os.Getenv("SGF2GIF_COOKIE")), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func parseRemoteInputSpec(input string) (remoteInputSpec, bool, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return remoteInputSpec{}, false, nil
	}

	lower := strings.ToLower(value)
	switch {
	case strings.HasPrefix(lower, "ogs-user:"):
		user := strings.TrimSpace(value[len("ogs-user:"):])
		if user == "" {
			return remoteInputSpec{}, false, fmt.Errorf("ogs-user source is missing a username or player id")
		}
		return remoteInputSpec{kind: remoteInputOGSUser, raw: value, user: user, provider: "ogs"}, true, nil
	case strings.HasPrefix(lower, "ogs:"):
		game := strings.TrimSpace(value[len("ogs:"):])
		if game == "" {
			return remoteInputSpec{}, false, fmt.Errorf("ogs source is missing a game id or URL")
		}
		if looksLikeURL(game) {
			return parseRemoteInputSpec(game)
		}
		return remoteInputSpec{kind: remoteInputOGSGame, raw: value, gameID: game, provider: "ogs"}, true, nil
	case strings.HasPrefix(lower, "fox:"):
		ref := strings.TrimSpace(value[len("fox:"):])
		if ref == "" {
			return remoteInputSpec{}, false, fmt.Errorf("fox source is missing a qipu url or page id")
		}
		if !looksLikeURL(ref) {
			ref = foxGamePageURL(defaultFoxBaseURL, ref)
		}
		return remoteInputSpec{kind: remoteInputFoxGame, raw: value, url: ref, provider: "fox"}, true, nil
	}

	if !looksLikeURL(value) {
		return remoteInputSpec{}, false, nil
	}

	u, err := url.Parse(value)
	if err != nil {
		return remoteInputSpec{}, false, err
	}
	host := strings.ToLower(u.Hostname())
	path := u.EscapedPath()
	switch {
	case strings.Contains(host, "online-go.com"):
		if gameID := parseOGSGameIDFromPath(path); gameID != "" {
			return remoteInputSpec{kind: remoteInputOGSGame, raw: value, url: value, gameID: gameID, provider: "ogs"}, true, nil
		}
	case strings.Contains(host, "foxwq.com"):
		if foxGameURLPattern.MatchString(path) {
			return remoteInputSpec{kind: remoteInputFoxGame, raw: value, url: value, provider: "fox"}, true, nil
		}
	}

	if strings.HasSuffix(strings.ToLower(path), ".sgf") {
		return remoteInputSpec{kind: remoteInputDirectSGF, raw: value, url: value, provider: "generic"}, true, nil
	}

	return remoteInputSpec{}, false, nil
}

func fetchRemoteSGFs(spec remoteInputSpec, settings sgfDownloadSettings) ([]sgfDownloadResult, error) {
	switch spec.kind {
	case remoteInputOGSGame:
		file, err := fetchOGSGameSGF(spec, settings)
		if err != nil {
			return nil, err
		}
		return []sgfDownloadResult{file}, nil
	case remoteInputOGSUser:
		return fetchOGSUserSGFs(spec, settings)
	case remoteInputFoxGame:
		file, err := fetchFoxGameSGF(spec, settings)
		if err != nil {
			return nil, err
		}
		return []sgfDownloadResult{file}, nil
	case remoteInputDirectSGF:
		file, err := fetchDirectSGF(spec.url, settings)
		if err != nil {
			return nil, err
		}
		return []sgfDownloadResult{file}, nil
	default:
		return nil, fmt.Errorf("unsupported remote input: %s", spec.raw)
	}
}

func fetchOGSGameSGF(spec remoteInputSpec, settings sgfDownloadSettings) (sgfDownloadResult, error) {
	gameID := spec.gameID
	if gameID == "" {
		gameID = parseOGSGameIDFromPath(spec.url)
	}
	if gameID == "" {
		return sgfDownloadResult{}, fmt.Errorf("could not determine OGS game id from %q", spec.raw)
	}

	targetURL := strings.TrimRight(settings.ogsBaseURL, "/") + "/api/v1/games/" + gameID + "/sgf"
	body, headers, err := fetchURLBytes(settings.client, targetURL, settings.cookie)
	if err != nil {
		return sgfDownloadResult{}, err
	}

	filename := filenameFromDisposition(headers.Get("Content-Disposition"))
	if filename == "" {
		filename = "ogs-" + gameID + ".sgf"
	}
	return sgfDownloadResult{
		filename: ensureSGFSuffix(filename),
		data:     body,
	}, nil
}

func fetchOGSUserSGFs(spec remoteInputSpec, settings sgfDownloadSettings) ([]sgfDownloadResult, error) {
	playerID, err := resolveOGSPlayerID(spec.user, settings)
	if err != nil {
		return nil, err
	}

	gameIDs, err := listOGSPlayerGameIDs(playerID, settings.limit, settings)
	if err != nil {
		return nil, err
	}
	results := make([]sgfDownloadResult, 0, len(gameIDs))
	for i, gameID := range gameIDs {
		fmt.Fprintf(os.Stdout, "\rDownloading OGS SGFs: %d/%d", i+1, len(gameIDs))
		file, err := fetchOGSGameSGF(remoteInputSpec{
			kind:     remoteInputOGSGame,
			gameID:   strconv.Itoa(gameID),
			provider: "ogs",
		}, settings)
		if err != nil {
			fmt.Fprintln(os.Stdout)
			return nil, err
		}
		results = append(results, file)
	}
	if len(gameIDs) > 0 {
		fmt.Fprintln(os.Stdout)
	}
	return results, nil
}

func resolveOGSPlayerID(user string, settings sgfDownloadSettings) (int, error) {
	if n, err := strconv.Atoi(user); err == nil && n > 0 {
		return n, nil
	}

	endpoint := strings.TrimRight(settings.ogsBaseURL, "/") + "/api/v1/players/?username=" + url.QueryEscape(user)
	body, _, err := fetchURLBytes(settings.client, endpoint, settings.cookie)
	if err != nil {
		return 0, err
	}
	var resp ogsPlayerLookupResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return 0, err
	}
	if len(resp.Results) == 0 {
		return 0, fmt.Errorf("could not find OGS user %q", user)
	}
	return resp.Results[0].ID, nil
}

func listOGSPlayerGameIDs(playerID, limit int, settings sgfDownloadSettings) ([]int, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("download limit must be positive")
	}

	pageURL := fmt.Sprintf("%s/api/v1/players/%d/games?ordering=-ended", strings.TrimRight(settings.ogsBaseURL, "/"), playerID)
	gameIDs := make([]int, 0, limit)
	seen := map[int]bool{}
	for pageURL != "" && len(gameIDs) < limit {
		body, _, err := fetchURLBytes(settings.client, pageURL, settings.cookie)
		if err != nil {
			return nil, err
		}
		var resp ogsPlayerGamesResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, err
		}
		for _, game := range resp.Results {
			if game.ID <= 0 || seen[game.ID] {
				continue
			}
			gameIDs = append(gameIDs, game.ID)
			seen[game.ID] = true
			if len(gameIDs) >= limit {
				break
			}
		}
		pageURL = strings.TrimSpace(resp.Next)
	}
	return gameIDs, nil
}

func fetchFoxGameSGF(spec remoteInputSpec, settings sgfDownloadSettings) (sgfDownloadResult, error) {
	targetURL := spec.url
	if targetURL == "" {
		targetURL = foxGamePageURL(settings.foxBaseURL, spec.gameID)
	}

	body, _, err := fetchURLBytes(settings.client, targetURL, settings.cookie)
	if err != nil {
		return sgfDownloadResult{}, err
	}
	htmlText := string(body)
	sgfText, err := extractFoxSGF(htmlText)
	if err != nil {
		return sgfDownloadResult{}, err
	}

	filename := extractFoxTitle(htmlText)
	if filename == "" {
		filename = "fox-" + foxGameIDFromURL(targetURL)
	}
	return sgfDownloadResult{
		filename: ensureSGFSuffix(sanitizeFilename(filename)),
		data:     []byte(sgfText),
	}, nil
}

func fetchDirectSGF(targetURL string, settings sgfDownloadSettings) (sgfDownloadResult, error) {
	body, headers, err := fetchURLBytes(settings.client, targetURL, settings.cookie)
	if err != nil {
		return sgfDownloadResult{}, err
	}
	filename := filenameFromDisposition(headers.Get("Content-Disposition"))
	if filename == "" {
		if parsed, err := url.Parse(targetURL); err == nil {
			filename = filepath.Base(parsed.Path)
		}
	}
	if filename == "" || filename == "." || filename == "/" {
		filename = "downloaded.sgf"
	}
	return sgfDownloadResult{
		filename: ensureSGFSuffix(filename),
		data:     body,
	}, nil
}

func fetchURLBytes(client *http.Client, targetURL, cookie string) ([]byte, http.Header, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", defaultDownloadUserAgent)
	if strings.TrimSpace(cookie) != "" {
		req.Header.Set("Cookie", cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, nil, fmt.Errorf("download failed for %s: %s %s", targetURL, resp.Status, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	return body, resp.Header.Clone(), nil
}

func extractFoxSGF(htmlText string) (string, error) {
	marker := `id="player-container">`
	start := strings.Index(htmlText, marker)
	if start < 0 {
		return "", fmt.Errorf("could not locate Fox qipu SGF block")
	}
	start += len(marker)
	rest := htmlText[start:]
	end := strings.Index(strings.ToLower(rest), "</div>")
	if end < 0 {
		return "", fmt.Errorf("could not find end of Fox qipu SGF block")
	}
	sgf := strings.TrimSpace(html.UnescapeString(rest[:end]))
	if !strings.HasPrefix(sgf, "(;") {
		return "", fmt.Errorf("Fox qipu page did not contain SGF content")
	}
	return sgf, nil
}

func extractFoxTitle(htmlText string) string {
	match := foxTitlePattern.FindStringSubmatch(htmlText)
	if len(match) != 2 {
		return ""
	}
	title := strings.TrimSpace(html.UnescapeString(match[1]))
	title = strings.TrimSuffix(title, "-野狐围棋")
	title = strings.TrimSpace(title)
	return title
}

func parseOGSGameIDFromPath(path string) string {
	if match := ogsGameURLPattern.FindStringSubmatch(path); len(match) == 2 {
		return match[1]
	}
	if match := ogsAPIURLPattern.FindStringSubmatch(path); len(match) >= 2 {
		return match[1]
	}
	return ""
}

func foxGameIDFromURL(targetURL string) string {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return ""
	}
	match := foxGameURLPattern.FindStringSubmatch(parsed.EscapedPath())
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func foxGamePageURL(baseURL, pageID string) string {
	return strings.TrimRight(baseURL, "/") + "/qipu/newlist/id/" + pageID + ".html"
}

func filenameFromDisposition(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}
	if name := strings.TrimSpace(params["filename"]); name != "" {
		return name
	}
	if name := strings.TrimSpace(params["filename*"]); name != "" {
		if idx := strings.LastIndex(name, "''"); idx >= 0 && idx+2 < len(name) {
			if decoded, err := url.QueryUnescape(name[idx+2:]); err == nil {
				return decoded
			}
		}
		return name
	}
	return ""
}

func ensureSGFSuffix(name string) string {
	name = sanitizeFilename(name)
	if !strings.HasSuffix(strings.ToLower(name), ".sgf") {
		name += ".sgf"
	}
	return name
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, ".sgf")
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "\\", "-")
	name = strings.ReplaceAll(name, ":", "-")
	name = strings.ReplaceAll(name, "\n", " ")
	name = strings.ReplaceAll(name, "\r", " ")
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "game"
	}

	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '_', r == '-', r == ' ', r > 127:
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	safe := strings.Trim(strings.ReplaceAll(b.String(), " ", "-"), "-.")
	if safe == "" {
		return "game"
	}
	return safe
}

func looksLikeURL(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func planDownloadOutputs(output string, results []sgfDownloadResult) ([]sgfDownloadOutput, error) {
	if len(results) == 0 {
		return nil, fmt.Errorf("no SGFs were downloaded")
	}

	if len(results) == 1 && !isDirectoryPathHint(output) {
		return []sgfDownloadOutput{{
			path: output,
			data: results[0].data,
		}}, nil
	}

	if info, err := os.Stat(output); err == nil && !info.IsDir() && len(results) > 1 {
		return nil, fmt.Errorf("output path %q must be a directory when downloading multiple SGFs", output)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return nil, err
	}

	outputs := make([]sgfDownloadOutput, 0, len(results))
	for _, result := range results {
		outputs = append(outputs, sgfDownloadOutput{
			path: filepath.Join(output, ensureSGFSuffix(result.filename)),
			data: result.data,
		})
	}
	return outputs, nil
}

func isDirectoryPathHint(path string) bool {
	if strings.HasSuffix(path, string(os.PathSeparator)) {
		return true
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return true
	}
	return false
}
