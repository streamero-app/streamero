import ImageIO
import SwiftUI
import UIKit

/// Loads artwork with the session token (AsyncImage cannot send headers) and caches it.
///
/// Pictures are decoded and downsampled off the main thread to the size they are shown at
/// (rounded up to a few buckets so a poster in a grid and on its page share an entry), and the
/// memory cache is bounded by decoded bytes. Downloads go through their own URL session with a
/// disk cache for artwork only. A cancelled view (scrolled away) cancels its download.
final class ImageCache: @unchecked Sendable {
    // @unchecked: the only state is the NSCache, which is documented as thread-safe.
    static let shared = ImageCache()

    private let cache: NSCache<NSString, UIImage> = {
        let cache = NSCache<NSString, UIImage>()
        cache.totalCostLimit = 128 * 1024 * 1024
        return cache
    }()

    private static let session: URLSession = {
        let config = URLSessionConfiguration.default
        let dir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask).first?
            .appending(path: "artwork", directoryHint: .isDirectory)
        config.urlCache = URLCache(memoryCapacity: 0, diskCapacity: 300 * 1024 * 1024, directory: dir)
        config.requestCachePolicy = .useProtocolCachePolicy
        return URLSession(configuration: config)
    }()

    /// Sizes pictures are decoded at (longest side, pixels).
    static let buckets: [CGFloat] = [256, 512, 1024, 2048, 4096]

    static func bucket(for pixels: CGFloat) -> CGFloat {
        guard pixels.isFinite, pixels > 0 else { return 1024 }
        return buckets.first { $0 >= pixels } ?? buckets[buckets.count - 1]
    }

    func cached(url: URL, maxPixels: CGFloat = 1024) -> UIImage? {
        cache.object(forKey: Self.key(url, Self.bucket(for: maxPixels)))
    }

    func image(url: URL, token: String, maxPixels: CGFloat = 1024) async -> UIImage? {
        let bucket = Self.bucket(for: maxPixels)
        let key = Self.key(url, bucket)
        if let hit = cache.object(forKey: key) { return hit }
        var req = URLRequest(url: url)
        req.timeoutInterval = 30
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        guard let (data, resp) = try? await Self.session.data(for: req),
              (resp as? HTTPURLResponse)?.statusCode == 200, !Task.isCancelled else { return nil }
        // Decoding is CPU work; it must not run on the main actor while a list scrolls.
        let decoded = await Task.detached(priority: .utility) { Self.decode(data, maxPixels: bucket) }.value
        guard let decoded else { return nil }
        cache.setObject(decoded, forKey: key, cost: Self.cost(of: decoded))
        return decoded
    }

    private static func key(_ url: URL, _ bucket: CGFloat) -> NSString {
        "\(url.absoluteString)#\(Int(bucket))" as NSString
    }

    private static func cost(of image: UIImage) -> Int {
        guard let cg = image.cgImage else { return 1 }
        return cg.bytesPerRow * cg.height
    }

    /// A decoded picture no larger than `maxPixels` on its longest side.
    static func decode(_ data: Data, maxPixels: CGFloat) -> UIImage? {
        let sourceOptions = [kCGImageSourceShouldCache: false] as CFDictionary
        guard let source = CGImageSourceCreateWithData(data as CFData, sourceOptions) else {
            return UIImage(data: data)
        }
        let options: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceShouldCacheImmediately: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceThumbnailMaxPixelSize: maxPixels,
        ]
        guard let cg = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary) else {
            return UIImage(data: data)
        }
        return UIImage(cgImage: cg)
    }
}

struct AuthImage: View {
    let url: URL?
    let token: String
    var title: String = ""
    var year: Int? = nil
    /// Fit instead of fill, with no placeholder (title logos).
    var fit = false
    /// The decode size; by default the size the view is laid out at.
    var maxPixels: CGFloat? = nil
    @State private var image: UIImage?
    @Environment(\.displayScale) private var displayScale
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        // A filled image would report its own (wider) size and push the whole screen past
        // the display edge; Color.clear takes the size the caller gives and the picture is
        // drawn into it.
        if fit {
            content(pixels: maxPixels ?? 1024)
        } else {
            Color.clear.overlay {
                GeometryReader { geo in
                    content(pixels: maxPixels ?? max(geo.size.width, geo.size.height) * displayScale)
                        .frame(width: geo.size.width, height: geo.size.height)
                }
            }
            .clipped()
        }
    }

    private func content(pixels: CGFloat) -> some View {
        ZStack {
            if !fit { PosterPlaceholder(title: title, year: year).opacity(image == nil ? 1 : 0) }
            if let image {
                if fit {
                    Image(uiImage: image).resizable().scaledToFit().transition(.opacity)
                } else {
                    Image(uiImage: image).resizable().scaledToFill().transition(.opacity)
                }
            }
        }
        .animation(reduceMotion ? nil : .easeOut(duration: 0.35), value: image != nil)
        .task(id: url) {
            guard let url else {
                image = nil
                return
            }
            // A picture already in memory shows at once, without the fade.
            if let hit = ImageCache.shared.cached(url: url, maxPixels: pixels) {
                image = hit
                return
            }
            let loaded = await ImageCache.shared.image(url: url, token: token, maxPixels: pixels)
            if !Task.isCancelled { image = loaded }
        }
    }
}

/// Typographic stand-in when an item has no artwork: the title set like a film card.
struct PosterPlaceholder: View {
    let title: String
    var year: Int? = nil

    var body: some View {
        let h = Theme.hue(for: title)
        ZStack(alignment: .bottomLeading) {
            RadialGradient(colors: [Color(hue: h, saturation: 0.22, brightness: 0.24),
                                    Color(hue: h, saturation: 0.14, brightness: 0.11), Theme.background],
                           center: .topLeading, startRadius: 0, endRadius: 320)
            if !title.isEmpty {
                VStack(alignment: .leading, spacing: 4) {
                    Text(title).displayFont(22).foregroundStyle(.white.opacity(0.95))
                        .lineLimit(4).minimumScaleFactor(0.7)
                    if let year { Text(String(year)).font(.caption2).foregroundStyle(.white.opacity(0.5)) }
                }
                .padding(12)
            }
        }
    }
}
