package stream

import "testing"

func TestShiftVTT(t *testing.T) {
	in := "WEBVTT\r\n\r\nNOTE 00:00:01.000 --> 00:00:02.000 is not a cue\r\n\r\n" +
		"1\r\n00:00:01.000 --> 00:00:04.000\r\nGone\r\n\r\n" +
		"00:09.500 --> 00:10.500 line:90%\r\nStraddles\r\n\r\n" +
		"01:00:12.250 --> 01:00:13.000\r\nLater\r\n"
	got := string(ShiftVTT([]byte(in), 10_000))
	want := "WEBVTT\n\nNOTE 00:00:01.000 --> 00:00:02.000 is not a cue\n\n" +
		"00:00:00.000 --> 00:00:00.500 line:90%\nStraddles\n\n" +
		"01:00:02.250 --> 01:00:03.000\nLater\n"
	if got != want {
		t.Fatalf("ShiftVTT:\n%q\nwant\n%q", got, want)
	}
}

func TestShiftVTTZeroOffsetIsIdentity(t *testing.T) {
	in := "WEBVTT\r\n\r\n00:00:01.000 --> 00:00:02.000\r\nHi\r\n"
	if got := string(ShiftVTT([]byte(in), 0)); got != in {
		t.Fatalf("offset 0 changed the document: %q", got)
	}
}

func TestParseVTTTime(t *testing.T) {
	for in, want := range map[string]int64{"00:00:01.5": 1500, "01:02.003": 62_003, "1:00:00.000": 3_600_000, "00:05": 5000} {
		if got, ok := parseVTTTime(in); !ok || got != want {
			t.Errorf("parseVTTTime(%q) = %d, %v; want %d", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "abc", "1.000", "-1:00.000"} {
		if _, ok := parseVTTTime(bad); ok {
			t.Errorf("parseVTTTime(%q) accepted", bad)
		}
	}
}
