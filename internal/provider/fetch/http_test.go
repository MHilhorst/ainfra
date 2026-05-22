package fetch

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchURLReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	}))
	defer srv.Close()
	got, err := FetchURL(srv.URL)
	if err != nil || string(got) != "hello" {
		t.Fatalf("FetchURL: %v / %q", err, got)
	}
}

func TestFetchURLErrorsOn404(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if _, err := FetchURL(srv.URL); err == nil {
		t.Error("FetchURL must error on 404")
	}
}
