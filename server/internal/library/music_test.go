package library

import "testing"

func TestReadTrackPrefersTagsAndFallsBackToFolders(t *testing.T) {
	f := planFile{path: "/m/Pink Floyd/The Wall (1979)/CD2/03 - Hey You.flac"}
	got := readTrack(f, map[string]string{})
	if got.title != "Hey You" || *got.track != 3 || got.album != "The Wall (1979)" || got.albumArtist != "Pink Floyd" || *got.disc != 2 {
		t.Fatalf("from folders: %+v", got)
	}
	tagged := readTrack(f, map[string]string{"title": "Hey You", "album": "The Wall", "artist": "Pink Floyd", "album_artist": "Pink Floyd",
		"track": "3/13", "disc": "2/2", "date": "1979-11-30", "genre": "Rock"})
	if tagged.album != "The Wall" || tagged.year != 1979 || *tagged.track != 3 || *tagged.disc != 2 {
		t.Fatalf("from tags: %+v", tagged)
	}
	comp := readTrack(planFile{path: "/m/VA/Hits/01.mp3"}, map[string]string{"title": "Song", "artist": "Guest", "album_artist": "Various Artists", "album": "Hits"})
	if comp.artist != "Guest" || comp.albumArtist != "Various Artists" {
		t.Fatalf("compilation: %+v", comp)
	}
	if musicKey("  The   Wall ") != "the wall" || leadingInt("x") != nil || *leadingInt("07/12") != 7 {
		t.Fatal("helpers")
	}
}
