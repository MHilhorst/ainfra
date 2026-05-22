package fetch

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// FetchURL retrieves the bytes at an http(s) URL. A non-2xx response is an
// error. It is the subscriber's artifact-download primitive.
func FetchURL(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch: GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch: GET %s: status %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
