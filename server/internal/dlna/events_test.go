package dlna

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// The initial GENA event goes to the subscriber only: a redirect from its
// callback must not send the request to another host.
func TestInitialEventDoesNotFollowRedirects(t *testing.T) {
	var third atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { third.Add(1) }))
	defer other.Close()
	var notified atomic.Int32
	subscriber := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		notified.Add(1)
		http.Redirect(w, r, other.URL+"/internal-admin", http.StatusTemporaryRedirect)
	}))
	defer subscriber.Close()
	s := &Server{}
	s.initialEvent(subscriber.URL+"/cb", "uuid:x", pathCMSEvents)
	if notified.Load() != 1 {
		t.Fatalf("subscriber notified %d times", notified.Load())
	}
	if third.Load() != 0 {
		t.Error("the event followed a redirect to another host")
	}
}
