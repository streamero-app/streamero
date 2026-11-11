package livetv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// HDHomeRun devices are used through their documented HTTP API:
// /discover.json (device), /lineup.json (channels) and the per-channel stream
// URL from the lineup (port 5004). A device with every tuner busy answers the
// stream request with 503.

// HDHRDevice is the answer of /discover.json.
type HDHRDevice struct {
	FriendlyName    string `json:"FriendlyName"`
	ModelNumber     string `json:"ModelNumber"`
	FirmwareName    string `json:"FirmwareName"`
	FirmwareVersion string `json:"FirmwareVersion"`
	DeviceID        string `json:"DeviceID"`
	BaseURL         string `json:"BaseURL"`
	LineupURL       string `json:"LineupURL"`
	TunerCount      int    `json:"TunerCount"`
}

// HDHRChannel is one entry of /lineup.json.
type HDHRChannel struct {
	GuideNumber string `json:"GuideNumber"`
	GuideName   string `json:"GuideName"`
	URL         string `json:"URL"`
	HD          int    `json:"HD"`
	Favorite    int    `json:"Favorite"`
	DRM         int    `json:"DRM"`
	VideoCodec  string `json:"VideoCodec"`
	AudioCodec  string `json:"AudioCodec"`
}

// hdhrBase normalises a device address ("192.168.1.20", "http://hdhr.local:80/").
func hdhrBase(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", ErrBadSource
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	u, err := httpURL(addr)
	if err != nil {
		return "", err
	}
	return u.Scheme + "://" + u.Host, nil
}

func getJSON(ctx context.Context, c *http.Client, u string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Streamero")
	resp, err := c.Do(req) // #nosec G704 -- admin-configured device; blocked addresses refused by the dialer
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, u)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(dst)
}

// HDHRDiscover reads the device description.
func HDHRDiscover(ctx context.Context, c *http.Client, addr string) (HDHRDevice, error) {
	base, err := hdhrBase(addr)
	if err != nil {
		return HDHRDevice{}, err
	}
	var d HDHRDevice
	if err := getJSON(ctx, c, base+"/discover.json", &d); err != nil {
		return HDHRDevice{}, err
	}
	if d.DeviceID == "" && d.TunerCount == 0 {
		return HDHRDevice{}, errors.New("not an HDHomeRun device")
	}
	if d.BaseURL == "" {
		d.BaseURL = base
	}
	return d, nil
}

// HDHRLineup reads the channel lineup (DRM-protected channels are left out:
// they cannot be decoded).
func HDHRLineup(ctx context.Context, c *http.Client, addr string, d HDHRDevice) ([]HDHRChannel, error) {
	base, err := hdhrBase(addr)
	if err != nil {
		return nil, err
	}
	lineup := base + "/lineup.json"
	if d.LineupURL != "" {
		// The device may name another host in LineupURL; only its path is used, so
		// requests always go to the configured address.
		if u, err := url.Parse(d.LineupURL); err == nil && u.Path != "" {
			lineup = base + u.Path
		}
	}
	var all []HDHRChannel
	if err := getJSON(ctx, c, lineup, &all); err != nil {
		return nil, err
	}
	out := all[:0]
	for _, ch := range all {
		if ch.DRM == 1 || ch.URL == "" {
			continue
		}
		out = append(out, ch)
	}
	return out, nil
}

// hdhrCodec maps lineup codec names to ffprobe codec names.
func hdhrCodec(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "MPEG2":
		return "mpeg2video"
	case "H264", "AVC":
		return "h264"
	case "HEVC", "H265":
		return "hevc"
	case "AC3":
		return "ac3"
	case "EAC3":
		return "eac3"
	case "AAC":
		return "aac"
	case "MPEG":
		return "mp2"
	}
	return ""
}

// ssdpSearch is the UPnP M-SEARCH request; HDHomeRun devices are UPnP media
// servers, so they answer it with their address.
const ssdpSearch = "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 2\r\nST: ssdp:all\r\n\r\n"

// DiscoverHDHR searches the local networks with SSDP and confirms every
// answering host through /discover.json. extra are hosts to try as well.
func DiscoverHDHR(ctx context.Context, c *http.Client, wait time.Duration, extra ...string) []HDHRDevice {
	hosts := map[string]bool{}
	for _, h := range extra {
		hosts[h] = true
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err == nil {
		dst := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
		for i := 0; i < 2; i++ {
			_, _ = conn.WriteToUDP([]byte(ssdpSearch), dst)
		}
		_ = conn.SetReadDeadline(time.Now().Add(wait))
		buf := make([]byte, 2048)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				break
			}
			if ip := from.IP; ip != nil && !blockedIP(ip) && strings.Contains(strings.ToUpper(string(buf[:n])), "HTTP/1.1 200") {
				hosts[ip.String()] = true
			}
		}
		_ = conn.Close()
	}
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out []HDHRDevice
	)
	seen := map[string]bool{}
	for h := range hosts {
		wg.Add(1)
		go func(h string) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			d, err := HDHRDiscover(cctx, c, h)
			if err != nil || d.DeviceID == "" {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if !seen[d.DeviceID] {
				seen[d.DeviceID] = true
				if d.BaseURL == "" {
					d.BaseURL = "http://" + h
				}
				out = append(out, d)
			}
		}(h)
	}
	wg.Wait()
	return out
}
