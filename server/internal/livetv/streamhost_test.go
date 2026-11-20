package livetv

import (
	"context"
	"errors"
	"testing"
)

// ffmpeg opens channel URLs itself, so CheckStreamHost must refuse the
// addresses the Go fetcher refuses before ffmpeg or ffprobe starts.
func TestCheckStreamHost(t *testing.T) {
	ctx := context.Background()
	for _, u := range []string{
		"http://127.0.0.1:5004/auto/v1",
		"http://localhost/stream.ts",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]/x",
		"http://[fe80::1]/x",
		"rtsp://0.0.0.0/live",
	} {
		if err := CheckStreamHost(ctx, u, false); !errors.Is(err, ErrBlockedAddress) {
			t.Errorf("%s: %v, want blocked", u, err)
		}
	}
	for _, u := range []string{"http://192.168.1.20:5004/auto/v1", "udp://@239.0.0.1:1234", "http://10.0.0.5/ch.ts"} {
		if err := CheckStreamHost(ctx, u, false); err != nil {
			t.Errorf("%s: %v, want allowed (LAN tuners)", u, err)
		}
	}
	if err := CheckStreamHost(ctx, "http://127.0.0.1:5004/x", true); err != nil {
		t.Errorf("loopback allowed for tests: %v", err)
	}
}
