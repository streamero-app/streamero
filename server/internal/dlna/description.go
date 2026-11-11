package dlna

import (
	"strings"
)

// Paths of the HTTP side.
const (
	PathDescription = "/dlna/description.xml"
	pathCDSSCPD     = "/dlna/cds.xml"
	pathCMSSCPD     = "/dlna/cms.xml"
	pathCDSControl  = "/dlna/ctl/cds"
	pathCMSControl  = "/dlna/ctl/cms"
	pathCDSEvents   = "/dlna/evt/cds"
	pathCMSEvents   = "/dlna/evt/cms"
)

// DeviceInfo is what the description document says about the server.
type DeviceInfo struct {
	UDN          string // "uuid:…"
	FriendlyName string
	Version      string
	Serial       string
}

// DeviceDescription renders the UPnP device description (MediaServer:1).
func DeviceDescription(d DeviceInfo) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` +
		`<root xmlns="urn:schemas-upnp-org:device-1-0" xmlns:dlna="urn:schemas-dlna-org:device-1-0" xmlns:sec="http://www.sec.co.kr/dlna">` +
		`<specVersion><major>1</major><minor>0</minor></specVersion><device>`)
	elem(&b, "deviceType", typeMediaServer)
	elem(&b, "friendlyName", d.FriendlyName)
	elem(&b, "manufacturer", "SCYDE")
	elem(&b, "manufacturerURL", "https://streamero.net")
	elem(&b, "modelDescription", "Streamero media server")
	elem(&b, "modelName", "Streamero")
	elem(&b, "modelNumber", d.Version)
	elem(&b, "modelURL", "https://streamero.net")
	elem(&b, "serialNumber", d.Serial)
	elem(&b, "UDN", d.UDN)
	b.WriteString(`<dlna:X_DLNADOC>DMS-1.50</dlna:X_DLNADOC>`)
	// Samsung TVs look for these before they ask for subtitle files.
	b.WriteString(`<sec:ProductCap>smi,DCM10,getMediaInfo.sec,getCaptionInfo.sec</sec:ProductCap>`)
	b.WriteString(`<sec:X_ProductCap>smi,DCM10,getMediaInfo.sec,getCaptionInfo.sec</sec:X_ProductCap>`)
	b.WriteString(`<serviceList>`)
	for _, s := range []struct{ typ, id, scpd, ctl, evt string }{
		{typeCDS, "urn:upnp-org:serviceId:ContentDirectory", pathCDSSCPD, pathCDSControl, pathCDSEvents},
		{typeCMS, "urn:upnp-org:serviceId:ConnectionManager", pathCMSSCPD, pathCMSControl, pathCMSEvents},
	} {
		b.WriteString(`<service>`)
		elem(&b, "serviceType", s.typ)
		elem(&b, "serviceId", s.id)
		elem(&b, "SCPDURL", s.scpd)
		elem(&b, "controlURL", s.ctl)
		elem(&b, "eventSubURL", s.evt)
		b.WriteString(`</service>`)
	}
	b.WriteString(`</serviceList></device></root>`)
	return b.String()
}

type arg struct{ name, dir, state string }
type action struct {
	name string
	args []arg
}
type stateVar struct {
	name, typ string
	events    bool
	allowed   []string
}

func scpd(actions []action, vars []stateVar) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?><scpd xmlns="urn:schemas-upnp-org:service-1-0">` +
		`<specVersion><major>1</major><minor>0</minor></specVersion><actionList>`)
	for _, a := range actions {
		b.WriteString(`<action><name>` + a.name + `</name><argumentList>`)
		for _, x := range a.args {
			b.WriteString(`<argument><name>` + x.name + `</name><direction>` + x.dir + `</direction><relatedStateVariable>` +
				x.state + `</relatedStateVariable></argument>`)
		}
		b.WriteString(`</argumentList></action>`)
	}
	b.WriteString(`</actionList><serviceStateTable>`)
	for _, v := range vars {
		ev := "no"
		if v.events {
			ev = "yes"
		}
		b.WriteString(`<stateVariable sendEvents="` + ev + `"><name>` + v.name + `</name><dataType>` + v.typ + `</dataType>`)
		if len(v.allowed) > 0 {
			b.WriteString(`<allowedValueList>`)
			for _, a := range v.allowed {
				b.WriteString(`<allowedValue>` + a + `</allowedValue>`)
			}
			b.WriteString(`</allowedValueList>`)
		}
		b.WriteString(`</stateVariable>`)
	}
	b.WriteString(`</serviceStateTable></scpd>`)
	return b.String()
}

// CDSDescription is the ContentDirectory:1 service description.
var CDSDescription = scpd([]action{
	{"GetSearchCapabilities", []arg{{"SearchCaps", "out", "SearchCapabilities"}}},
	{"GetSortCapabilities", []arg{{"SortCaps", "out", "SortCapabilities"}}},
	{"GetSystemUpdateID", []arg{{"Id", "out", "SystemUpdateID"}}},
	{"Browse", []arg{{"ObjectID", "in", "A_ARG_TYPE_ObjectID"}, {"BrowseFlag", "in", "A_ARG_TYPE_BrowseFlag"},
		{"Filter", "in", "A_ARG_TYPE_Filter"}, {"StartingIndex", "in", "A_ARG_TYPE_Index"},
		{"RequestedCount", "in", "A_ARG_TYPE_Count"}, {"SortCriteria", "in", "A_ARG_TYPE_SortCriteria"},
		{"Result", "out", "A_ARG_TYPE_Result"}, {"NumberReturned", "out", "A_ARG_TYPE_Count"},
		{"TotalMatches", "out", "A_ARG_TYPE_Count"}, {"UpdateID", "out", "A_ARG_TYPE_UpdateID"}}},
	{"Search", []arg{{"ContainerID", "in", "A_ARG_TYPE_ObjectID"}, {"SearchCriteria", "in", "A_ARG_TYPE_SearchCriteria"},
		{"Filter", "in", "A_ARG_TYPE_Filter"}, {"StartingIndex", "in", "A_ARG_TYPE_Index"},
		{"RequestedCount", "in", "A_ARG_TYPE_Count"}, {"SortCriteria", "in", "A_ARG_TYPE_SortCriteria"},
		{"Result", "out", "A_ARG_TYPE_Result"}, {"NumberReturned", "out", "A_ARG_TYPE_Count"},
		{"TotalMatches", "out", "A_ARG_TYPE_Count"}, {"UpdateID", "out", "A_ARG_TYPE_UpdateID"}}},
	// Samsung extensions: where to start browsing, and the resume bookmark the TV sets on stop.
	{"X_GetFeatureList", []arg{{"FeatureList", "out", "A_ARG_TYPE_Featurelist"}}},
	{"X_SetBookmark", []arg{{"CategoryType", "in", "A_ARG_TYPE_CategoryType"}, {"RID", "in", "A_ARG_TYPE_RID"},
		{"ObjectID", "in", "A_ARG_TYPE_ObjectID"}, {"PosSecond", "in", "A_ARG_TYPE_PosSec"}}},
}, []stateVar{
	{name: "SearchCapabilities", typ: "string"},
	{name: "SortCapabilities", typ: "string"},
	{name: "SystemUpdateID", typ: "ui4", events: true},
	{name: "A_ARG_TYPE_ObjectID", typ: "string"},
	{name: "A_ARG_TYPE_Result", typ: "string"},
	{name: "A_ARG_TYPE_SearchCriteria", typ: "string"},
	{name: "A_ARG_TYPE_BrowseFlag", typ: "string", allowed: []string{"BrowseMetadata", "BrowseDirectChildren"}},
	{name: "A_ARG_TYPE_Filter", typ: "string"},
	{name: "A_ARG_TYPE_SortCriteria", typ: "string"},
	{name: "A_ARG_TYPE_Index", typ: "ui4"},
	{name: "A_ARG_TYPE_Count", typ: "ui4"},
	{name: "A_ARG_TYPE_UpdateID", typ: "ui4"},
	{name: "A_ARG_TYPE_Featurelist", typ: "string"},
	{name: "A_ARG_TYPE_CategoryType", typ: "ui4"},
	{name: "A_ARG_TYPE_RID", typ: "ui4"},
	{name: "A_ARG_TYPE_PosSec", typ: "ui4"},
})

// CMSDescription is the ConnectionManager:1 service description.
var CMSDescription = scpd([]action{
	{"GetProtocolInfo", []arg{{"Source", "out", "SourceProtocolInfo"}, {"Sink", "out", "SinkProtocolInfo"}}},
	{"GetCurrentConnectionIDs", []arg{{"ConnectionIDs", "out", "CurrentConnectionIDs"}}},
	{"GetCurrentConnectionInfo", []arg{{"ConnectionID", "in", "A_ARG_TYPE_ConnectionID"},
		{"RcsID", "out", "A_ARG_TYPE_RcsID"}, {"AVTransportID", "out", "A_ARG_TYPE_AVTransportID"},
		{"ProtocolInfo", "out", "A_ARG_TYPE_ProtocolInfo"}, {"PeerConnectionManager", "out", "A_ARG_TYPE_ConnectionManager"},
		{"PeerConnectionID", "out", "A_ARG_TYPE_ConnectionID"}, {"Direction", "out", "A_ARG_TYPE_Direction"},
		{"Status", "out", "A_ARG_TYPE_ConnectionStatus"}}},
}, []stateVar{
	{name: "SourceProtocolInfo", typ: "string", events: true},
	{name: "SinkProtocolInfo", typ: "string", events: true},
	{name: "CurrentConnectionIDs", typ: "string", events: true},
	{name: "A_ARG_TYPE_ConnectionStatus", typ: "string", allowed: []string{"OK", "ContentFormatMismatch", "InsufficientBandwidth", "UnreliableChannel", "Unknown"}},
	{name: "A_ARG_TYPE_ConnectionManager", typ: "string"},
	{name: "A_ARG_TYPE_Direction", typ: "string", allowed: []string{"Input", "Output"}},
	{name: "A_ARG_TYPE_ProtocolInfo", typ: "string"},
	{name: "A_ARG_TYPE_ConnectionID", typ: "i4"},
	{name: "A_ARG_TYPE_AVTransportID", typ: "i4"},
	{name: "A_ARG_TYPE_RcsID", typ: "i4"},
})

// sourceProtocols is what GetProtocolInfo announces.
func sourceProtocols() string {
	mimes := []string{"video/mp4", "video/x-matroska", "video/x-mkv", "video/mpeg", "video/webm", "video/quicktime",
		"video/avi", "video/x-msvideo", "audio/mpeg", "audio/mp4", "audio/flac", "audio/wav", "audio/ogg", "image/jpeg", "text/srt"}
	out := make([]string, len(mimes))
	for i, m := range mimes {
		out[i] = "http-get:*:" + m + ":*"
	}
	return strings.Join(out, ",")
}
