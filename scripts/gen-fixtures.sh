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

echo "done: $OUT"
