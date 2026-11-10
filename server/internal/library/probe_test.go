package library

import "testing"

func TestParseFFprobe(t *testing.T) {
	out := []byte(`{"format":{"format_name":"matroska,webm","duration":"12.345"},"streams":[
	 {"codec_type":"video","codec_name":"hevc","width":3840,"height":2160,"color_transfer":"smpte2084","profile":"Main 10","level":153,"pix_fmt":"yuv420p10le"},
	 {"codec_type":"video","codec_name":"mjpeg","disposition":{"attached_pic":1}},
	 {"codec_type":"audio","codec_name":"eac3","channels":6,"tags":{"language":"eng","title":"Surround"}},
	 {"codec_type":"audio","codec_name":"aac","channels":2,"tags":{"language":"und"}},
	 {"codec_type":"subtitle","codec_name":"subrip","tags":{"language":"hrv"},"disposition":{"forced":1}}]}`)
	r, err := parseFFprobe(out, "/m/x.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if r.Container != "mkv" || r.DurationMS != 12345 || r.VideoCodec != "hevc" || r.Width != 3840 || r.HDR != "HDR10" {
		t.Fatalf("video: %+v", r)
	}
	if r.VideoProfile != "Main 10" || r.VideoLevel != 153 || r.VideoBitDepth != 10 {
		t.Fatalf("video profile: %+v", r)
	}
	if bitDepth("8", "yuv420p10le") != 8 || bitDepth("", "yuv420p") != 8 || bitDepth("", "") != 0 || bitDepth("", "yuv444p12le") != 12 {
		t.Fatal("bit depth")
	}
	if len(r.Audio) != 2 || r.Audio[0].Language != "eng" || r.Audio[0].Channels != 6 || r.Audio[1].Language != "" {
		t.Fatalf("audio: %+v", r.Audio)
	}
	if len(r.Subtitles) != 1 || !r.Subtitles[0].Forced || r.Subtitles[0].Language != "hrv" {
		t.Fatalf("subs: %+v", r.Subtitles)
	}
	dv := []byte(`{"format":{"format_name":"mov,mp4,m4a,3gp,3g2,mj2"},"streams":[{"codec_type":"video","codec_name":"hevc","side_data_list":[{"side_data_type":"DOVI configuration record"}]}]}`)
	if r, _ := parseFFprobe(dv, "/m/x.mp4"); r.HDR != "Dolby Vision" || r.Container != "mp4" {
		t.Fatalf("dv: %+v", r)
	}
	if _, err := parseFFprobe([]byte("not json"), "/x"); err == nil {
		t.Fatal("bad output accepted")
	}
}

func TestExternalSubtitle(t *testing.T) {
	v := "/m/Big Test Movie (2021)/Big Test Movie (2021).mkv"
	cases := map[string]SubtitleStream{
		"/m/Big Test Movie (2021)/Big Test Movie (2021).en.srt":        {Codec: "srt", Language: "en", External: true},
		"/m/Big Test Movie (2021)/Big Test Movie (2021).hr.forced.srt": {Codec: "srt", Language: "hr", Forced: true, External: true},
		"/m/Big Test Movie (2021)/Big Test Movie (2021).ass":           {Codec: "ass", External: true},
	}
	for p, want := range cases {
		got, ok := ExternalSubtitle(v, p)
		want.Path = p
		if !ok || got != want {
			t.Errorf("%s: %+v %v, want %+v", p, got, ok, want)
		}
	}
	if _, ok := ExternalSubtitle(v, "/m/Big Test Movie (2021)/Other.en.srt"); ok {
		t.Error("unrelated subtitle matched")
	}
	// Versions share the subtitles of the base name.
	if s, ok := ExternalSubtitle("/m/X (2021)/X (2021) - 2160p.mkv", "/m/X (2021)/X (2021).hr.srt"); !ok || s.Language != "hr" {
		t.Errorf("version did not get base subtitle: %+v %v", s, ok)
	}
	// Name boundary.
	if _, ok := ExternalSubtitle("/m/Movie 20.mkv", "/m/Movie 2.srt"); ok {
		t.Error("Movie 2.srt matched Movie 20.mkv")
	}
}
