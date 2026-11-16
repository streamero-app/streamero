# Brand icon

`icon-source.png` is the owner's icon mockup (the "S" film-strip mark on a black tile).
`scripts/brand-icons.py` crops the tile, paints its rounded corners with the tile's own
black so the icon is full-bleed (iOS and Android apply their own mask), and writes every size:

| Output (`docs/brand/out/`) | Goes to |
|---|---|
| `icon-1024.png` | `ios/Streamero/Resources/Assets.xcassets/AppIcon.appiconset/AppIcon-1024.png` |
| `AppMark.png` | `ios/Streamero/Resources/Assets.xcassets/AppMark.imageset/AppMark.png` (in-app mark) |
| `ic_launcher-<density>.png` | `android/app/src/main/res/mipmap-<density>/ic_launcher.png` |
| `banner-xhdpi.png`, `banner-xxhdpi.png` | `android/app/src/main/res/drawable-*/banner.png` (Android TV) |
| `icon-192.png`, `icon-512.png`, `apple-touch-icon.png` | `web/public/` |
| `favicon-64.png` | `web/public/favicon.png` |
| `mark-256.png` | `web/public/mark.png` (in-app mark on the web) |

The mockup is 465 px across, so the 1024 px icon is upscaled; a larger original (1024 px or
vector) can replace `icon-source.png` and the script re-run for a sharper result.
