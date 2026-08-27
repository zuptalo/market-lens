package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSPAHandlerServesFilesAndFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("shell"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("asset"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		path string
		want int
		body string
	}{
		{path: "/dashboard", want: http.StatusOK, body: "shell"},
		{path: "/assets/app.js", want: http.StatusOK, body: "asset"},
		{path: "/assets/missing.js", want: http.StatusNotFound},
		{path: "/api/v1/missing", want: http.StatusNotFound},
	} {
		recorder := httptest.NewRecorder()
		spaHandler(dir).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tt.path, nil))
		if recorder.Code != tt.want || (tt.body != "" && recorder.Body.String() != tt.body) {
			t.Fatalf("path=%s status=%d body=%q", tt.path, recorder.Code, recorder.Body.String())
		}
	}
}
