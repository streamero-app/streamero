package dlna

import (
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Service types.
const (
	typeMediaServer = "urn:schemas-upnp-org:device:MediaServer:1"
	typeCDS         = "urn:schemas-upnp-org:service:ContentDirectory:1"
	typeCMS         = "urn:schemas-upnp-org:service:ConnectionManager:1"
)

// UPnP error codes (UPnP Device Architecture and ContentDirectory:1).
const (
	errInvalidAction = 401
	errInvalidArgs   = 402
	errActionFailed  = 501
	errNoSuchObject  = 701
	errCannotProcess = 720
)

// soapError is a UPnP fault.
type soapError struct {
	Code int
	Desc string
}

func (e *soapError) Error() string { return e.Desc }

// Action is a parsed SOAP request: action name and its arguments.
type Action struct {
	Service string // service type from the SOAPACTION header
	Name    string
	Args    map[string]string
}

var errBadSOAP = errors.New("malformed SOAP request")

// ParseAction reads a SOAP control request (body at most 64 KiB).
func ParseAction(r *http.Request) (Action, error) {
	var a Action
	// SOAPACTION: "urn:schemas-upnp-org:service:ContentDirectory:1#Browse"
	sa := strings.Trim(strings.TrimSpace(r.Header.Get("SOAPACTION")), `"`)
	if svc, name, ok := strings.Cut(sa, "#"); ok {
		a.Service, a.Name = svc, name
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		return a, err
	}
	name, args, err := parseEnvelope(body)
	if err != nil {
		return a, err
	}
	if a.Name == "" {
		a.Name = name
	} else if name != "" && name != a.Name {
		return a, errBadSOAP
	}
	a.Args = args
	return a, nil
}

// parseEnvelope returns the action element's local name and its child values.
func parseEnvelope(body []byte) (string, map[string]string, error) {
	d := xml.NewDecoder(strings.NewReader(string(body)))
	d.Strict = false
	depth, bodyDepth := 0, -1
	action := ""
	args := map[string]string{}
	var cur string
	var text strings.Builder
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, errBadSOAP
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			switch {
			case bodyDepth < 0 && t.Name.Local == "Body":
				bodyDepth = depth
			case bodyDepth > 0 && depth == bodyDepth+1:
				action = t.Name.Local
			case bodyDepth > 0 && depth == bodyDepth+2:
				cur = t.Name.Local
				text.Reset()
			}
		case xml.CharData:
			if cur != "" {
				text.Write(t)
			}
		case xml.EndElement:
			if bodyDepth > 0 && depth == bodyDepth+2 && cur != "" {
				args[cur] = text.String()
				cur = ""
			}
			depth--
		}
	}
	if action == "" {
		return "", nil, errBadSOAP
	}
	return action, args, nil
}

// pair is one output argument, in order.
type pair struct{ k, v string }

// writeSOAP answers an action with its output arguments.
func writeSOAP(w http.ResponseWriter, service, action string, out []pair) {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` +
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	b.WriteString(`<u:` + action + `Response xmlns:u="` + service + `">`)
	for _, p := range out {
		b.WriteString("<" + p.k + ">" + esc(p.v) + "</" + p.k + ">")
	}
	b.WriteString(`</u:` + action + `Response></s:Body></s:Envelope>`)
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	setExact(w.Header(), "EXT", "")
	_, _ = io.WriteString(w, b.String())
}

// writeFault answers with a UPnP error.
func writeFault(w http.ResponseWriter, e *soapError) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>`+
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`+
		`<s:Fault><faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail>`+
		`<UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>`+strconv.Itoa(e.Code)+`</errorCode>`+
		`<errorDescription>`+esc(e.Desc)+`</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`)
}
