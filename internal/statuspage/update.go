package statuspage

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ReleasesURL is the public release list the update check reads (nothing is sent).
var ReleasesURL = "https://api.github.com/repos/mayagurutech/tallyguru/releases?per_page=5"

// DownloadPrefix is the only place an installer is downloaded from.
var DownloadPrefix = "https://github.com/mayagurutech/tallyguru/releases/download/"

const setupName, sumsName = "TallyMCP-Setup.exe", "SHA256SUMS.txt"

type release struct {
	Tag    string `json:"tag_name"`
	URL    string `json:"html_url"`
	Draft  bool   `json:"draft"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (r release) asset(name string) string {
	for _, a := range r.Assets {
		if a.Name == name && strings.HasPrefix(a.URL, DownloadPrefix) {
			return a.URL
		}
	}
	return ""
}

func get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "ledgerguru") // generic client name, no user details
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = fmt.Errorf("file larger than expected")
	}
	return b, err
}

// latest returns the newest published release (pre-releases included).
func latest(ctx context.Context) (release, error) {
	b, err := get(ctx, ReleasesURL, 1<<20)
	if err != nil {
		return release{}, err
	}
	var rs []release
	if err := json.Unmarshal(b, &rs); err != nil {
		return release{}, fmt.Errorf("could not read the release info")
	}
	var best release
	for _, r := range rs {
		if !r.Draft && (best.Tag == "" || Newer(r.Tag, best.Tag)) {
			best = r
		}
	}
	if best.Tag == "" {
		return release{}, fmt.Errorf("no public release yet")
	}
	return best, nil
}

// Newer reports whether version a is later than b ("v0.1.0" > "0.1.0-beta.2" > "0.1.0-beta.1").
func Newer(a, b string) bool { return cmpVer(a, b) > 0 }

func cmpVer(a, b string) int {
	a, b = strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v")
	am, ap, _ := strings.Cut(a, "-")
	bm, bp, _ := strings.Cut(b, "-")
	if c := cmpParts(am, bm); c != 0 {
		return c
	}
	switch {
	case ap == bp:
		return 0
	case ap == "":
		return 1
	case bp == "":
		return -1
	}
	return cmpParts(ap, bp)
}

func cmpParts(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		if i >= len(as) {
			return -1
		}
		if i >= len(bs) {
			return 1
		}
		x, ex := strconv.Atoi(as[i])
		y, ey := strconv.Atoi(bs[i])
		switch {
		case ex == nil && ey == nil && x != y:
			if x > y {
				return 1
			}
			return -1
		case (ex != nil || ey != nil) && as[i] != bs[i]:
			if as[i] > bs[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

// sumFor finds name's SHA-256 in a SHA256SUMS file.
func sumFor(sums []byte, name string) string {
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0])
		}
	}
	return ""
}

// startInstaller runs the downloaded installer quietly. It closes running copies (this panel and
// the AI app's server) and reopens the control panel when done. Replaced in tests.
var startInstaller = func(path string) error {
	return exec.Command(path, "/SILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/CLOSEAPPLICATIONS", "/FORCECLOSEAPPLICATIONS").Start()
}

func addUpdateRoutes(mux *http.ServeMux, version string, post func(*http.Request) bool, stop func()) {
	deny := func(w http.ResponseWriter) { http.Error(w, "forbidden", http.StatusForbidden) }
	type answer struct {
		Msg   string
		Newer bool
		Tag   string
	}
	reply := func(w http.ResponseWriter, a answer) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(a)
	}
	mux.HandleFunc("/update", func(w http.ResponseWriter, r *http.Request) {
		if !post(r) {
			deny(w)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		rel, err := latest(ctx)
		if err != nil {
			reply(w, answer{Msg: fmt.Sprintf("Could not check (%v). You are on %s.", err, version)})
			return
		}
		if !Newer(rel.Tag, version) {
			reply(w, answer{Msg: fmt.Sprintf("You have the latest version (%s).", version)})
			return
		}
		a := answer{Tag: rel.Tag, Newer: rel.asset(setupName) != "" && rel.asset(sumsName) != ""}
		a.Msg = fmt.Sprintf("Version %s is available (you have %s).", rel.Tag, version)
		if !a.Newer {
			a.Msg += " Download it from " + rel.URL
		}
		reply(w, a)
	})
	mux.HandleFunc("/update/install", func(w http.ResponseWriter, r *http.Request) {
		if !post(r) {
			deny(w)
			return
		}
		fail := func(f string, v ...any) { http.Error(w, fmt.Sprintf(f, v...), http.StatusBadGateway) }
		if strings.HasSuffix(version, "-dev") {
			http.Error(w, "This is a development build; update it from the source code.", http.StatusConflict)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		tag, path, err := FetchUpdate(ctx, version)
		if err != nil {
			fail("%v", err)
			return
		}
		if path == "" {
			fmt.Fprintf(w, "You already have the latest version (%s).", version)
			return
		}
		if err := startInstaller(path); err != nil {
			http.Error(w, "Could not start the installer: "+err.Error(), http.StatusInternalServerError)
			return
		}
		rel := release{Tag: tag}
		fmt.Fprintf(w, "Installing %s. This page will close; the control panel reopens by itself when the update is done. Then fully quit and reopen your AI app.", rel.Tag)
		go func() { time.Sleep(time.Second); stop() }()
	})
}

// FetchUpdate downloads the newest release's installer when it is newer than version and checks
// it against the published checksum. path is "" when already up to date. Only public release
// files from this project's GitHub page are read; nothing is sent.
func FetchUpdate(ctx context.Context, version string) (tag, path string, err error) {
	rel, err := latest(ctx)
	if err != nil {
		return "", "", fmt.Errorf("could not check: %w", err)
	}
	if !Newer(rel.Tag, version) {
		return rel.Tag, "", nil
	}
	su, iu := rel.asset(sumsName), rel.asset(setupName)
	if su == "" || iu == "" {
		return rel.Tag, "", fmt.Errorf("release %s has no installer; download it from %s", rel.Tag, rel.URL)
	}
	sums, err := get(ctx, su, 64<<10)
	if err != nil {
		return rel.Tag, "", fmt.Errorf("could not download the checksums: %w", err)
	}
	want := sumFor(sums, setupName)
	setup, err := get(ctx, iu, 100<<20)
	if err != nil {
		return rel.Tag, "", fmt.Errorf("could not download the installer: %w", err)
	}
	got := sha256.Sum256(setup)
	if want == "" || hex.EncodeToString(got[:]) != want {
		return rel.Tag, "", fmt.Errorf("the downloaded installer did not match its published checksum, so it was not run; download it from %s", rel.URL)
	}
	dir, err := os.MkdirTemp("", "tallyguru-update-")
	if err != nil {
		return rel.Tag, "", err
	}
	path = filepath.Join(dir, setupName)
	return rel.Tag, path, os.WriteFile(path, setup, 0o700)
}
