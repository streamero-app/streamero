#!/usr/bin/env bash
# Regenerates the "Streamero Manage" launcher icons (all mipmap densities) and TV banners
# from the Streamero icon. Run from android/ with a JDK and the DejaVu fonts available.
set -euo pipefail
for d in mdpi hdpi xhdpi xxhdpi xxxhdpi; do
  java -Djava.awt.headless=true tools/ManageIcon.java "app/src/main/res/mipmap-$d/ic_launcher.png" "app/src/manage/res/mipmap-$d/ic_launcher.png"
done
mkdir -p app/src/manage/res/drawable-xhdpi app/src/manage/res/drawable-xxhdpi
font=/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf
java -Djava.awt.headless=true tools/Banner.java app/src/manage/res/mipmap-xxxhdpi/ic_launcher.png app/src/manage/res/drawable-xhdpi/banner.png 1 "$font"
java -Djava.awt.headless=true tools/Banner.java app/src/manage/res/mipmap-xxxhdpi/ic_launcher.png app/src/manage/res/drawable-xxhdpi/banner.png 1.5 "$font"
