package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRemoteInputSpec(t *testing.T) {
	tests := []struct {
		input    string
		wantKind remoteInputKind
		wantID   string
		wantUser string
		wantOK   bool
	}{
		{input: "ogs:85130272", wantKind: remoteInputOGSGame, wantID: "85130272", wantOK: true},
		{input: "ogs-user:rainoffallingstar1234", wantKind: remoteInputOGSUser, wantUser: "rainoffallingstar1234", wantOK: true},
		{input: "https://online-go.com/game/85130272", wantKind: remoteInputOGSGame, wantID: "85130272", wantOK: true},
		{input: "https://online-go.com/api/v1/games/85130272/sgf", wantKind: remoteInputOGSGame, wantID: "85130272", wantOK: true},
		{input: "https://www.foxwq.com/qipu/newlist/id/2026031862241631.html", wantKind: remoteInputFoxGame, wantOK: true},
		{input: "https://example.com/game.sgf", wantKind: remoteInputDirectSGF, wantOK: true},
		{input: "local-file.sgf", wantKind: remoteInputUnknown, wantOK: false},
	}

	for _, tt := range tests {
		spec, ok, err := parseRemoteInputSpec(tt.input)
		if err != nil {
			t.Fatalf("parseRemoteInputSpec(%q) returned error: %v", tt.input, err)
		}
		if ok != tt.wantOK {
			t.Fatalf("parseRemoteInputSpec(%q) ok=%v, want %v", tt.input, ok, tt.wantOK)
		}
		if !ok {
			continue
		}
		if spec.kind != tt.wantKind {
			t.Fatalf("parseRemoteInputSpec(%q) kind=%v, want %v", tt.input, spec.kind, tt.wantKind)
		}
		if spec.gameID != tt.wantID {
			t.Fatalf("parseRemoteInputSpec(%q) gameID=%q, want %q", tt.input, spec.gameID, tt.wantID)
		}
		if spec.user != tt.wantUser {
			t.Fatalf("parseRemoteInputSpec(%q) user=%q, want %q", tt.input, spec.user, tt.wantUser)
		}
	}
}

func TestExtractFoxSGF(t *testing.T) {
	htmlText := `
		<html>
		<title>示例对局-野狐围棋</title>
		<div class="panel-body eidogo-player-auto modal-content" id="player-container">
			(;GM[1]FF[4]SZ[19];B[pd];W[dd])
		</div>
		</html>
	`

	sgfText, err := extractFoxSGF(htmlText)
	if err != nil {
		t.Fatalf("extractFoxSGF returned error: %v", err)
	}
	if sgfText != "(;GM[1]FF[4]SZ[19];B[pd];W[dd])" {
		t.Fatalf("unexpected SGF text: %q", sgfText)
	}
}

func TestFetchOGSUserSGFs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/players/" && r.URL.Query().Get("username") == "alice":
			fmt.Fprint(w, `{"count":1,"results":[{"id":42,"username":"alice"}]}`)
		case r.URL.Path == "/api/v1/players/42/games":
			fmt.Fprint(w, `{"next":"","results":[{"id":101},{"id":102},{"id":103}]}`)
		case r.URL.Path == "/api/v1/games/101/sgf":
			w.Header().Set("Content-Disposition", "attachment; filename=alice-101.sgf")
			fmt.Fprint(w, "(;GM[1]FF[4]SZ[19];B[aa])")
		case r.URL.Path == "/api/v1/games/102/sgf":
			w.Header().Set("Content-Disposition", "attachment; filename=alice-102.sgf")
			fmt.Fprint(w, "(;GM[1]FF[4]SZ[19];B[bb])")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	results, err := fetchOGSUserSGFs(remoteInputSpec{
		kind: remoteInputOGSUser,
		user: "alice",
	}, sgfDownloadSettings{
		client:     server.Client(),
		limit:      2,
		ogsBaseURL: server.URL,
		foxBaseURL: defaultFoxBaseURL,
	})
	if err != nil {
		t.Fatalf("fetchOGSUserSGFs returned error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d SGFs, want 2", len(results))
	}
	if results[0].filename != "alice-101.sgf" {
		t.Fatalf("first filename = %q", results[0].filename)
	}
	if !strings.Contains(string(results[1].data), ";B[bb]") {
		t.Fatalf("second SGF data = %q", string(results[1].data))
	}
}

func TestFetchFoxGameSGF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `
			<html>
			<title>测试棋谱-野狐围棋</title>
			<div class="panel-body eidogo-player-auto modal-content" id="player-container">
				(;GM[1]FF[4]SZ[19];B[pd];W[dd])
			</div>
			</html>
		`)
	}))
	defer server.Close()

	result, err := fetchFoxGameSGF(remoteInputSpec{
		kind: remoteInputFoxGame,
		url:  server.URL + "/qipu/newlist/id/2026031862241631.html",
	}, sgfDownloadSettings{
		client:     server.Client(),
		limit:      1,
		ogsBaseURL: defaultOGSBaseURL,
		foxBaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("fetchFoxGameSGF returned error: %v", err)
	}
	if result.filename != "测试棋谱.sgf" {
		t.Fatalf("filename = %q, want %q", result.filename, "测试棋谱.sgf")
	}
	if !strings.Contains(string(result.data), ";W[dd]") {
		t.Fatalf("unexpected SGF data: %q", string(result.data))
	}
}

func TestPlanDownloadOutputs(t *testing.T) {
	single, err := planDownloadOutputs("/tmp/out.sgf", []sgfDownloadResult{{filename: "a.sgf", data: []byte("a")}})
	if err != nil {
		t.Fatalf("planDownloadOutputs single returned error: %v", err)
	}
	if len(single) != 1 || single[0].path != "/tmp/out.sgf" {
		t.Fatalf("unexpected single outputs: %#v", single)
	}

	dir := t.TempDir()
	multi, err := planDownloadOutputs(dir, []sgfDownloadResult{
		{filename: "a.sgf", data: []byte("a")},
		{filename: "b.sgf", data: []byte("b")},
	})
	if err != nil {
		t.Fatalf("planDownloadOutputs multi returned error: %v", err)
	}
	if len(multi) != 2 {
		t.Fatalf("got %d outputs, want 2", len(multi))
	}
	if multi[0].path != filepath.Join(dir, "a.sgf") {
		t.Fatalf("first multi path = %q", multi[0].path)
	}
}

func TestLoadDownloadCookie(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cookie.txt")
	if err := os.WriteFile(path, []byte("sid=123; token=abc\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	cookie, err := loadDownloadCookie(path)
	if err != nil {
		t.Fatalf("loadDownloadCookie returned error: %v", err)
	}
	if cookie != "sid=123; token=abc" {
		t.Fatalf("cookie = %q", cookie)
	}
}

func TestParseArgsAcceptsDownloadFlags(t *testing.T) {
	cookiePath := filepath.Join(t.TempDir(), "cookie.txt")
	if err := os.WriteFile(cookiePath, []byte("sid=xyz"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	oldArgs := os.Args
	defer func() {
		os.Args = oldArgs
	}()

	os.Args = []string{
		"sgf2gif",
		"--download-sgf",
		"--download-limit", "12",
		"--download-cookie-file", cookiePath,
		"ogs-user:alice",
		"/tmp/out",
	}

	opts, err := parseArgs()
	if err != nil {
		t.Fatalf("parseArgs returned error: %v", err)
	}
	if !opts.downloadSGF {
		t.Fatal("downloadSGF = false, want true")
	}
	if opts.downloadLimit != 12 {
		t.Fatalf("downloadLimit = %d, want 12", opts.downloadLimit)
	}
	if opts.downloadCookie != "sid=xyz" {
		t.Fatalf("downloadCookie = %q, want %q", opts.downloadCookie, "sid=xyz")
	}
}
