package dlna

import (
	"bytes"
	"encoding/xml"
	"strconv"
	"strings"
)

// UPnP classes used for the tree.
const (
	classFolder  = "object.container.storageFolder"
	classArtist  = "object.container.person.musicArtist"
	classAlbum   = "object.container.album.musicAlbum"
	classGenre   = "object.container.genre"
	classMovie   = "object.item.videoItem.movie"
	classVideo   = "object.item.videoItem"
	classTrack   = "object.item.audioItem.musicTrack"
	classAudio   = "object.item.audioItem"
	classPicture = "object.item.imageItem"
)

// Res is one way to fetch an item (DIDL-Lite <res>).
type Res struct {
	URL          string
	ProtocolInfo string
	Size         int64
	DurationMS   int64
	Resolution   string // "1920x1080"
	Channels     int
}

// Object is a DIDL-Lite container or item.
type Object struct {
	ID, ParentID string
	Title        string
	Class        string
	Container    bool
	ChildCount   int // containers; -1 = unknown (attribute left out)
	Date         string
	Artist       string
	Album        string
	Genre        string
	TrackNumber  int
	ArtURL       string
	Res          []Res
	// Subtitle is a sidecar subtitle URL (SRT) for TVs that take one.
	Subtitle string
	// SubtitleRes adds the subtitle as an extra <res>; CaptionInfoEx adds sec:CaptionInfoEx.
	SubtitleRes, CaptionInfoEx bool
	// Description is a short text (overview) some TVs show.
	Description string
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func attr(b *strings.Builder, name, v string) {
	b.WriteString(" " + name + `="` + esc(v) + `"`)
}

func elem(b *strings.Builder, name, v string) {
	if v == "" {
		return
	}
	b.WriteString("<" + name + ">" + esc(v) + "</" + name + ">")
}

// DIDL renders objects as a DIDL-Lite document.
func DIDL(objs []Object) string {
	var b strings.Builder
	b.WriteString(`<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/"` +
		` xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" xmlns:dlna="urn:schemas-dlna-org:metadata-1-0/" xmlns:sec="http://www.sec.co.kr/">`)
	for _, o := range objs {
		writeObject(&b, o)
	}
	b.WriteString(`</DIDL-Lite>`)
	return b.String()
}

func writeObject(b *strings.Builder, o Object) {
	tag := "item"
	if o.Container {
		tag = "container"
	}
	b.WriteString("<" + tag)
	attr(b, "id", o.ID)
	attr(b, "parentID", o.ParentID)
	attr(b, "restricted", "1")
	if o.Container {
		attr(b, "searchable", "0")
		if o.ChildCount >= 0 {
			attr(b, "childCount", strconv.Itoa(o.ChildCount))
		}
	}
	b.WriteString(">")
	elem(b, "dc:title", o.Title)
	elem(b, "upnp:class", o.Class)
	elem(b, "dc:date", o.Date)
	if o.Artist != "" {
		elem(b, "upnp:artist", o.Artist)
		elem(b, "dc:creator", o.Artist)
	}
	elem(b, "upnp:album", o.Album)
	elem(b, "upnp:genre", o.Genre)
	elem(b, "dc:description", o.Description)
	if o.TrackNumber > 0 {
		elem(b, "upnp:originalTrackNumber", strconv.Itoa(o.TrackNumber))
	}
	if o.ArtURL != "" {
		b.WriteString(`<upnp:albumArtURI dlna:profileID="` + thumbnailProfile + `">` + esc(o.ArtURL) + `</upnp:albumArtURI>`)
	}
	for _, r := range o.Res {
		b.WriteString("<res")
		attr(b, "protocolInfo", r.ProtocolInfo)
		if r.Size > 0 {
			attr(b, "size", strconv.FormatInt(r.Size, 10))
		}
		if r.DurationMS > 0 {
			attr(b, "duration", Duration(r.DurationMS))
		}
		if r.Resolution != "" {
			attr(b, "resolution", r.Resolution)
		}
		if r.Channels > 0 {
			attr(b, "nrAudioChannels", strconv.Itoa(r.Channels))
		}
		b.WriteString(">" + esc(r.URL) + "</res>")
	}
	if o.Subtitle != "" {
		if o.SubtitleRes {
			b.WriteString(`<res protocolInfo="http-get:*:text/srt:*">` + esc(o.Subtitle) + `</res>`)
		}
		if o.CaptionInfoEx {
			b.WriteString(`<sec:CaptionInfoEx sec:type="srt">` + esc(o.Subtitle) + `</sec:CaptionInfoEx>`)
		}
	}
	b.WriteString("</" + tag + ">")
}
