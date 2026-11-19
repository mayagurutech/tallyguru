package statuspage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v0.1.0-beta.2", "0.1.0-beta.1", true},
		{"v0.1.0", "0.1.0-beta.2", true},
		{"v0.1.0-beta.10", "0.1.0-beta.9", true},
		{"v0.2.0", "0.1.9", true},
		{"v0.1.0-beta.2", "0.1.0-beta.2", false},
		{"v0.1.0-beta.2", "0.1.0", false},
		{"v0.1.0-beta.2", "0.1.0-dev", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}

// fakeGitHub serves a release list, checksums and an installer whose bytes may differ from the sum.
func fakeGitHub(t *testing.T, tag, setup, summed string) {
	sum := sha256.Sum256([]byte(summed))
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			base := srv.URL + "/dl/" + tag + "/"
			json.NewEncoder(w).Encode([]map[string]any{
				{"tag_name": "v0.0.9", "assets": []any{}},
				{"tag_name": tag, "html_url": srv.URL + "/page", "assets": []map[string]string{
					{"name": setupName, "browser_download_url": base + setupName},
					{"name": sumsName, "browser_download_url": base + sumsName}}},
			})
		case "/dl/" + tag + "/" + sumsName:
			fmt.Fprintf(w, "%s  %s\nffff  tallyguru.exe\n", hex.EncodeToString(sum[:]), setupName)
		case "/dl/" + tag + "/" + setupName:
			w.Write([]byte(setup))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldR, oldD, oldS := ReleasesURL, DownloadPrefix, startInstaller
	ReleasesURL, DownloadPrefix = srv.URL+"/releases", srv.URL+"/dl/"
	t.Cleanup(func() { ReleasesURL, DownloadPrefix, startInstaller = oldR, oldD, oldS })
}

func updateMux(version string) (*http.ServeMux, *bool) {
	stopped := false
	mux := http.NewServeMux()
	addUpdateRoutes(mux, version, func(r *http.Request) bool { return r.Method == http.MethodPost }, func() { stopped = true })
	return mux, &stopped
}

func call(mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
	return rec
}

func TestUpdateCheckAndInstall(t *testing.T) {
	fakeGitHub(t, "v0.1.0-beta.3", "INSTALLER", "INSTALLER")
	var ran string
	startInstaller = func(p string) error {
		b, _ := os.ReadFile(p)
		ran = string(b)
		return nil
	}
	mux, _ := updateMux("0.1.0-beta.2")
	var a struct {
		Msg   string
		Newer bool
		Tag   string
	}
	json.Unmarshal(call(mux, "/update").Body.Bytes(), &a)
	if !a.Newer || a.Tag != "v0.1.0-beta.3" {
		t.Fatalf("check: %+v", a)
	}
	rec := call(mux, "/update/install")
	if rec.Code != 200 || ran != "INSTALLER" || !strings.Contains(rec.Body.String(), "Installing v0.1.0-beta.3") {
		t.Fatalf("install: %d %q ran=%q", rec.Code, rec.Body, ran)
	}

	same, _ := updateMux("0.1.0-beta.3")
	json.Unmarshal(call(same, "/update").Body.Bytes(), &a)
	if a.Newer || !strings.Contains(a.Msg, "latest") {
		t.Errorf("up to date: %+v", a)
	}
	if rec := call(same, "/update"); rec.Code != 200 {
		t.Error("check failed")
	}
	get := httptest.NewRecorder()
	same.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/update/install", nil))
	if get.Code != http.StatusForbidden {
		t.Error("GET must be refused")
	}
}

func TestUpdateRefusesTamperedInstaller(t *testing.T) {
	fakeGitHub(t, "v0.1.0-beta.3", "EVIL", "INSTALLER")
	startInstaller = func(string) error { t.Fatal("tampered installer was run"); return nil }
	mux, stopped := updateMux("0.1.0-beta.2")
	rec := call(mux, "/update/install")
	if rec.Code == 200 || !strings.Contains(rec.Body.String(), "did not match") || *stopped {
		t.Fatalf("tampered: %d %q", rec.Code, rec.Body)
	}
}

func TestUpdateRefusesDevBuild(t *testing.T) {
	fakeGitHub(t, "v0.1.0-beta.3", "INSTALLER", "INSTALLER")
	startInstaller = func(string) error { t.Fatal("dev build ran installer"); return nil }
	mux, _ := updateMux("0.1.0-dev")
	if rec := call(mux, "/update/install"); rec.Code != http.StatusConflict {
		t.Fatalf("dev: %d %q", rec.Code, rec.Body)
	}
}
