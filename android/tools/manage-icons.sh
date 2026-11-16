#!/usr/bin/env bash
# Regenerates the "Streamero Manage" launcher icons (all mipmap densities) and TV banners
# from the brand icon (docs/brand/icon-1024.png). Icon only, no text. Run from android/ with a JDK:
#   docker run --rm -u 1000:1000 -v "$PWD/..":/w -w /w/android ghcr.io/cirruslabs/android-sdk:35 bash tools/manage-icons.sh
set -euo pipefail
src="${1:-../docs/brand/icon-1024.png}"
run() { java -Djava.awt.headless=true tools/ManageIcon.java "$@"; }
for d in mdpi:48 hdpi:72 xhdpi:96 xxhdpi:144 xxxhdpi:192; do
  run icon "$src" "app/src/manage/res/mipmap-${d%%:*}/ic_launcher.png" "${d##*:}"
done
run banner "$src" app/src/manage/res/drawable-xhdpi/banner.png 320 180
run banner "$src" app/src/manage/res/drawable-xxhdpi/banner.png 480 270
