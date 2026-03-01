package relay

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSpaHandler_ServesFile(t *testing.T) {
	mapFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>app</html>")},
		"style.css":  &fstest.MapFile{Data: []byte("body{}")},
	}
	handler := spaHandler(http.FileServerFS(mapFS), mapFS)

	r := httptest.NewRequest(http.MethodGet, "/style.css", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if !strings.Contains(string(body), "body{}") {
		t.Errorf("response body %q does not contain %q", string(body), "body{}")
	}
}

func TestSpaHandler_FallbackToIndex(t *testing.T) {
	mapFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>app</html>")},
	}
	handler := spaHandler(http.FileServerFS(mapFS), mapFS)

	r := httptest.NewRequest(http.MethodGet, "/some/route", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if !strings.Contains(string(body), "<html>app</html>") {
		t.Errorf("response body %q does not contain %q", string(body), "<html>app</html>")
	}
}

func TestSpaHandler_Root(t *testing.T) {
	mapFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html>home</html>")},
	}
	handler := spaHandler(http.FileServerFS(mapFS), mapFS)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if !strings.Contains(string(body), "<html>home</html>") {
		t.Errorf("response body %q does not contain %q", string(body), "<html>home</html>")
	}
}

func TestStaticHandler_NoDistDir(t *testing.T) {
	s := &Server{hub: NewHub(slog.Default()), logger: slog.Default()}

	handler := s.StaticHandler()

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if !strings.Contains(string(body), "Web UI not built") {
		t.Errorf("response body %q does not contain %q", string(body), "Web UI not built")
	}
}
