package netx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSafeTransport(t *testing.T) {
	c := &http.Client{Transport: SafeTransport(), Timeout: 5 * time.Second}
	for _, u := range []string{"http://169.254.169.254/latest/meta-data/", "http://[fe80::1]/", "http://0.0.0.0:1/", "http://224.0.0.1/"} {
		_, err := c.Get(u) //nolint:noctx // test
		if !errors.Is(err, ErrBlockedAddress) {
			t.Errorf("%s: %v, want ErrBlockedAddress", u, err)
		}
	}
	// Services on this machine (loopback) and the LAN stay reachable.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	defer srv.Close()
	resp, err := c.Get(srv.URL) //nolint:noctx // test
	if err != nil {
		t.Fatalf("loopback service: %v", err)
	}
	_ = resp.Body.Close()
	// A redirect to the metadata address is refused at the connection.
	redir := httptest.NewServer(http.RedirectHandler("http://169.254.169.254/", http.StatusFound))
	defer redir.Close()
	if _, err := c.Get(redir.URL); !errors.Is(err, ErrBlockedAddress) { //nolint:noctx // test
		t.Errorf("redirect to metadata: %v", err)
	}
}
