#!/usr/bin/env bash
# Generates small synthetic test media with ffmpeg, laid out like a real library.
# Usage: scripts/gen-fixtures.sh [OUT_DIR] (default ./data/fixtures). Idempotent: existing files are kept.
set -euo pipefail
OUT="${1:-./data/fixtures}"
DUR="${FIXTURE_DURATION:-12}"
command -v ffmpeg >/dev/null || { echo "ffmpeg is required" >&2; exit 1; }
ENCODERS="$(ffmpeg -hide_banner -encoders 2>/dev/null)"
has_enc() { grep -q " $1 " <<<"$ENCODERS"; }

FF=(ffmpeg -hide_banner -loglevel error -nostdin -y)
SRC_V=(-f lavfi -i "testsrc2=size=1280x720:rate=24:duration=${DUR}")
SRC_A=(-f lavfi -i "sine=frequency=440:sample_rate=48000:duration=${DUR}")

make() { # make <path> <ffmpeg args...>
  local path="$1"; shift
  [[ -f "$path" ]] && return 0
  mkdir -p "$(dirname "$path")"
  echo "  $path"
  "${FF[@]}" "$@" "$path"
}

srt() { # srt <path> <text>
  [[ -f "$1" ]] && return 0
  mkdir -p "$(dirname "$1")"
  printf '1\n00:00:01,000 --> 00:00:04,000\n%s\n\n2\n00:00:05,000 --> 00:00:08,000\n%s (2)\n' "$2" "$2" > "$1"
}

ass() { # ass <path>
  [[ -f "$1" ]] && return 0
  cat > "$1" <<'ASS'
[Script Info]
ScriptType: v4.00+
PlayResX: 1280
PlayResY: 720

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, OutlineColour, BackColour, Bold, Italic, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Arial,48,&H00FFFFFF,&H00000000,&H64000000,0,0,1,2,0,2,20,20,40,1

[Events]
Format: Layer, Start, End, Style, Text
Dialogue: 0,0:00:01.00,0:00:05.00,Default,{\an8\c&H00FFFF&}Styled ASS subtitle
ASS
}

echo "==> movies"
M="$OUT/movies"
make "$M/Big Test Movie (2021)/Big Test Movie (2021).mkv" "${SRC_V[@]}" "${SRC_A[@]}" \
  -c:v libx264 -preset veryfast -crf 28 -c:a aac -b:a 128k -metadata:s:a:0 language=eng
srt "$M/Big Test Movie (2021)/Big Test Movie (2021).en.srt" "English subtitle"
srt "$M/Big Test Movie (2021)/Big Test Movie (2021).hr.forced.srt" "Prisilni hrvatski podnaslov"

# Multi-audio + embedded subtitles + chapters.
CHAP="$(mktemp)"; trap 'rm -f "$CHAP"' EXIT
# Chapter boundary at a third of the clip, so short clips (FIXTURE_DURATION=3) stay valid.
SPLIT=$((DUR * 1000 / 3))
printf ';FFMETADATA1\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=%d\ntitle=Intro\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=%d\nEND=%d\ntitle=Main\n' "$SPLIT" "$SPLIT" "$((DUR*1000))" > "$CHAP"
SUB_TMP="$(mktemp --suffix=.srt)"; srt_tmp_text="Embedded subtitle"
printf '1\n00:00:01,000 --> 00:00:04,000\n%s\n' "$srt_tmp_text" > "$SUB_TMP"
make "$M/Multi Audio Film (2019)/Multi Audio Film (2019).mkv" "${SRC_V[@]}" "${SRC_A[@]}" \
  -f lavfi -i "sine=frequency=660:sample_rate=48000:duration=${DUR}" -i "$SUB_TMP" -i "$CHAP" \
  -map 0:v -map 1:a -map 2:a -map 3:s -map_metadata 4 \
  -c:v libx264 -preset veryfast -crf 28 -c:a:0 aac -ac:a:0 2 -c:a:1 ac3 -ac:a:1 6 -c:s srt \
  -metadata:s:a:0 language=eng -metadata:s:a:0 title="Stereo" \
  -metadata:s:a:1 language=hrv -metadata:s:a:1 title="5.1" -metadata:s:s:0 language=eng
rm -f "$SUB_TMP"

ASS_DIR="$M/Anime Style Movie (2020)"; mkdir -p "$ASS_DIR"; ass "$ASS_DIR/Anime Style Movie (2020).en.ass"
make "$ASS_DIR/Anime Style Movie (2020).mp4" "${SRC_V[@]}" "${SRC_A[@]}" \
  -c:v libx264 -preset veryfast -crf 28 -c:a aac -movflags +faststart

if has_enc libx265; then
  make "$M/HEVC Ten Bit (2022)/HEVC Ten Bit (2022) - 2160p.mkv" -f lavfi -i "testsrc2=size=3840x2160:rate=24:duration=${DUR}" "${SRC_A[@]}" \
    -c:v libx265 -preset ultrafast -crf 32 -pix_fmt yuv420p10le -x265-params log-level=error -c:a aac
  # HDR10 metadata (PQ / BT.2020) for tonemapping tests.
  make "$M/HDR Sample (2023)/HDR Sample (2023).mkv" -f lavfi -i "testsrc2=size=1920x1080:rate=24:duration=${DUR}" "${SRC_A[@]}" \
    -c:v libx265 -preset ultrafast -crf 32 -pix_fmt yuv420p10le \
    -x265-params "log-level=error:hdr10=1:colorprim=bt2020:transfer=smpte2084:colormatrix=bt2020nc:master-display=G(13250,34500)B(7500,3000)R(34000,16000)WP(15635,16450)L(10000000,1):max-cll=1000,400" \
    -color_primaries bt2020 -color_trc smpte2084 -colorspace bt2020nc -c:a aac
  # Same movie, second version.
  make "$M/Big Test Movie (2021)/Big Test Movie (2021) - 2160p.mkv" -f lavfi -i "testsrc2=size=3840x2160:rate=24:duration=${DUR}" "${SRC_A[@]}" \
    -c:v libx265 -preset ultrafast -crf 34 -c:a aac
else
  echo "  (skipping HEVC fixtures: libx265 not available)"
fi
if has_enc libsvtav1; then
  make "$M/AV1 Sample (2024)/AV1 Sample (2024).mp4" "${SRC_V[@]}" "${SRC_A[@]}" -c:v libsvtav1 -preset 12 -crf 45 -c:a libopus
elif has_enc libaom-av1; then
  make "$M/AV1 Sample (2024)/AV1 Sample (2024).mp4" "${SRC_V[@]}" "${SRC_A[@]}" -c:v libaom-av1 -cpu-used 8 -crf 50 -c:a libopus
fi
make "$M/Big Test Movie (2021)/trailers/Official Trailer.mp4" -f lavfi -i "testsrc2=size=640x360:rate=24:duration=4" \
  -f lavfi -i "sine=frequency=880:duration=4" -c:v libx264 -preset veryfast -c:a aac

echo "==> shows"
S="$OUT/shows/Test Show (2020)"
# Every episode shares the same 6 s "intro" (identical audio + video) for intro detection tests.
INTRO_V="testsrc=size=1280x720:rate=24:duration=6"
INTRO_A="aevalsrc=0.3*sin(2*PI*(300+200*t)*t):s=48000:d=6"
for se in 01 02; do
  for ep in 01 02 03 04; do
    f="$S/Season $se/Test Show S${se}E${ep}.mkv"
    freq=$((200 + 10#$se * 100 + 10#$ep * 37))
    make "$f" -f lavfi -i "$INTRO_V" -f lavfi -i "$INTRO_A" \
      -f lavfi -i "testsrc2=size=1280x720:rate=24:duration=${DUR}" -f lavfi -i "sine=frequency=${freq}:sample_rate=48000:duration=${DUR}" \
      -filter_complex "[0:v][1:a][2:v][3:a]concat=n=2:v=1:a=1[v][a]" -map "[v]" -map "[a]" \
      -c:v libx264 -preset veryfast -crf 28 -c:a aac
  done
done
make "$S/Season 01/Test Show S01E05-E06.mkv" "${SRC_V[@]}" "${SRC_A[@]}" -c:v libx264 -preset veryfast -crf 28 -c:a aac
make "$S/Specials/Test Show S00E01.mkv" "${SRC_V[@]}" "${SRC_A[@]}" -c:v libx264 -preset veryfast -crf 28 -c:a aac
make "$OUT/shows/Daily Show/Daily Show 2024-03-15.mp4" "${SRC_V[@]}" "${SRC_A[@]}" -c:v libx264 -preset veryfast -crf 28 -c:a aac
make "$OUT/shows/Anime Absolute/[Group] Anime Absolute - 101 [1080p].mkv" "${SRC_V[@]}" "${SRC_A[@]}" -c:v libx264 -preset veryfast -crf 28 -c:a aac

echo "==> music"
A="$OUT/music/Test Artist/Test Album (2018)"
for n in 1 2 3; do
  make "$A/0${n} - Track ${n}.flac" -f lavfi -i "sine=frequency=$((300 + n * 110)):sample_rate=44100:duration=8" \
    -metadata artist="Test Artist" -metadata album_artist="Test Artist" -metadata album="Test Album" \
    -metadata title="Track ${n}" -metadata track="${n}/3" -metadata date=2018 -metadata genre=Electronic -c:a flac
done
make "$OUT/music/Test Artist/Single (2020)/01 - Single.mp3" -f lavfi -i "sine=frequency=523:duration=8" \
  -metadata artist="Test Artist" -metadata album="Single" -metadata title="Single" -c:a libmp3lame -b:a 192k
make "$A/cover.jpg" -f lavfi -i "testsrc2=size=600x600:duration=1" -frames:v 1

echo "==> photos"
for n in 1 2; do make "$OUT/photos/Holiday 2025/IMG_000${n}.jpg" -f lavfi -i "testsrc2=size=1600x1200:duration=1" -frames:v 1; done
make "$OUT/photos/Holiday 2025/Beach/IMG_0010.jpg" -f lavfi -i "color=c=0x2a7ab0:size=1200x800:duration=1" -frames:v 1
make "$OUT/photos/Holiday 2025/Beach/CLIP_0011.mp4" -f lavfi -i "testsrc2=size=640x360:rate=24:duration=3" \
  -c:v libx264 -preset veryfast -crf 30 -pix_fmt yuv420p -metadata creation_time=2025-07-14T10:00:00Z
make "$OUT/photos/loose.png" -f lavfi -i "color=c=0xb07a2a:size=400x400:duration=1" -frames:v 1

# zipfile <out> <dir>: a stored (uncompressed) ZIP of the files under dir, "mimetype" first
# (the EPUB rule). Perl, because the runtime image has no zip tool; CRC-32 is computed here.
zipfile() {
  [[ -f "$1" ]] && return 0
  mkdir -p "$(dirname "$1")"
  local abs; abs="$(cd "$(dirname "$1")" && pwd)/$(basename "$1")"
  echo "  $1"
  (cd "$2" && { [[ -f mimetype ]] && echo mimetype; find . -type f ! -name mimetype | sed 's|^\./||' | sort; } | perl -e '
    my @t; for my $n (0..255) { my $c = $n; for (1..8) { $c = ($c & 1) ? (0xEDB88320 ^ ($c >> 1)) : ($c >> 1) } $t[$n] = $c }
    sub crc { my $c = 0xFFFFFFFF; $c = $t[($c ^ ord($_)) & 0xFF] ^ ($c >> 8) for split //, $_[0]; return $c ^ 0xFFFFFFFF }
    my ($out, $cd, $n) = ("", "", 0);
    while (my $f = <STDIN>) { chomp $f; open my $h, "<:raw", $f or die; local $/; my $d = <$h>; close $h;
      my $c = crc($d); my $l = length $d;
      $cd .= pack("VvvvvvvVVVvvvvvVV", 0x02014b50, 20, 10, 0, 0, 0, 0, $c, $l, $l, length $f, 0, 0, 0, 0, 0, length $out) . $f;
      $out .= pack("VvvvvvVVVvv", 0x04034b50, 10, 0, 0, 0, 0, $c, $l, $l, length $f, 0) . $f . $d; $n++ }
    binmode STDOUT; print $out, $cd, pack("VvvvvVVv", 0x06054b50, 0, 0, $n, $n, length $cd, length $out, 0);' > "$abs")
}

echo "==> books"
T="$(mktemp -d)"
mkdir -p "$T/epub/META-INF" "$T/epub/OEBPS" "$T/comic"
printf 'application/epub+zip' > "$T/epub/mimetype"
printf '<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>' > "$T/epub/META-INF/container.xml"
cat > "$T/epub/OEBPS/content.opf" <<'OPF'
<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="uid">
 <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
  <dc:identifier id="uid">urn:isbn:9780306406157</dc:identifier><dc:title>The Long Way</dc:title>
  <dc:creator id="a1">Ana Novak</dc:creator><dc:language>en</dc:language><dc:date>2019</dc:date>
  <dc:description>A short test novel in two chapters.</dc:description>
  <meta property="dcterms:modified">2024-01-01T00:00:00Z</meta>
  <meta property="belongs-to-collection" id="s1">Wayfarers</meta><meta refines="#s1" property="group-position">1</meta>
 </metadata>
 <manifest>
  <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
  <item id="cover" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
  <item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/><item id="c2" href="c2.xhtml" media-type="application/xhtml+xml"/>
  <item id="css" href="style.css" media-type="text/css"/>
 </manifest>
 <spine><itemref idref="c1"/><itemref idref="c2"/></spine>
</package>
OPF
printf '<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops"><head><title>Contents</title></head><body><nav epub:type="toc"><ol><li><a href="c1.xhtml">One</a></li><li><a href="c2.xhtml">Two</a></li></ol></nav></body></html>' > "$T/epub/OEBPS/nav.xhtml"
for c in 1 2; do
  { printf '<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml"><head><title>Chapter %s</title><link rel="stylesheet" type="text/css" href="style.css"/></head><body><h1>Chapter %s</h1>' "$c" "$c"
    for p in $(seq 1 30); do printf '<p>Paragraph %s of chapter %s. The ship drifted between the stars while the crew argued about dinner.</p>' "$p" "$c"; done
    printf '</body></html>'; } > "$T/epub/OEBPS/c$c.xhtml"
done
# A book stylesheet: the reader loads it from a blob: URL, which the web client's CSP must allow.
printf 'h1 { letter-spacing: 3px; }\n' > "$T/epub/OEBPS/style.css"
"${FF[@]}" -f lavfi -i "color=c=0x7a2a2a:size=600x900:duration=1" -frames:v 1 "$T/epub/OEBPS/cover.jpg"
zipfile "$OUT/books/Ana Novak/The Long Way.epub" "$T/epub"
printf '<?xml version="1.0"?><ComicInfo><Series>Space Cats</Series><Number>1</Number><Writer>Mia Writer</Writer><Manga>YesAndRightToLeft</Manga><Summary>Cats, in space.</Summary></ComicInfo>' > "$T/comic/ComicInfo.xml"
for n in 1 2 3 4; do "${FF[@]}" -f lavfi -i "testsrc2=size=800x1200:duration=1" -frames:v 1 "$T/comic/page0${n}.jpg"; done
zipfile "$OUT/books/Space Cats/Space Cats 001.cbz" "$T/comic"
CBT="$OUT/books/Space Cats/Space Cats 002.cbt"
[[ -f "$CBT" ]] || { CBT_ABS="$(cd "$(dirname "$CBT")" && pwd)/$(basename "$CBT")"; (cd "$T/comic" && tar -cf "$CBT_ABS" page01.jpg page02.jpg); echo "  $CBT"; }
if [[ ! -f "$OUT/books/plain.pdf" ]]; then
  echo "  $OUT/books/plain.pdf"
  perl -e 'my @o = ("<< /Type /Catalog /Pages 2 0 R >>", "<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>");
    push @o, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 400] /Contents 5 0 R /Resources << /Font << /F1 6 0 R >> >> >>" for 1..2;
    my $s = "BT /F1 24 Tf 40 300 Td (Streamero PDF) Tj ET"; push @o, "<< /Length " . length($s) . " >>\nstream\n$s\nendstream";
    push @o, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>", "<< /Title (Plain Document) /Author (Pat Doe) /Producer (gen-fixtures) >>";
    my $b = "%PDF-1.4\n"; my @x; for my $i (0..$#o) { push @x, length $b; $b .= ($i + 1) . " 0 obj\n$o[$i]\nendobj\n" }
    my $xr = length $b; $b .= "xref\n0 " . (@o + 1) . "\n0000000000 65535 f \n"; $b .= sprintf("%010d 00000 n \n", $_) for @x;
    $b .= "trailer\n<< /Size " . (@o + 1) . " /Root 1 0 R /Info " . scalar(@o) . " 0 R >>\nstartxref\n$xr\n%%EOF\n"; print $b' > "$OUT/books/plain.pdf"
fi
rm -rf "$T"

echo "==> audiobooks"
AB="$OUT/audiobooks"
if [[ ! -f "$AB/Lea Author/The Sound Book.m4b" ]]; then
  mkdir -p "$AB/Lea Author"
  CH="$(mktemp)"
  printf ';FFMETADATA1\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=4000\ntitle=Opening\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=4000\nEND=8000\ntitle=Closing\n' > "$CH"
  make "$AB/Lea Author/The Sound Book.m4b" -f lavfi -i "sine=frequency=392:duration=8" -i "$CH" -map_metadata 1 -map 0 \
    -metadata album="The Sound Book" -metadata artist="Lea Author" -metadata composer="Nina Narrator" -c:a aac -b:a 64k -f mp4
  rm -f "$CH"
fi
for n in 1 2; do
  make "$AB/Max Author/Folder Book/0${n} - Part ${n}.mp3" -f lavfi -i "sine=frequency=$((250 + n * 60)):duration=5" \
    -metadata album="Folder Book" -metadata artist="Max Author" -metadata title="Part ${n}" -metadata track="${n}" -c:a libmp3lame -b:a 64k
done
make "$AB/Max Author/Folder Book/cover.jpg" -f lavfi -i "color=c=0x2a7a4a:size=600x600:duration=1" -frames:v 1

echo "done: $OUT"
