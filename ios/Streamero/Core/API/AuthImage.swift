import SwiftUI
import UIKit

/// Loads artwork with the session token (AsyncImage cannot send headers) and caches it.
@MainActor
final class ImageCache {
    static let shared = ImageCache()
    private let cache = NSCache<NSURL, UIImage>()

    func image(url: URL, token: String) async -> UIImage? {
        if let hit = cache.object(forKey: url as NSURL) { return hit }
        var req = URLRequest(url: url)
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        guard let (data, resp) = try? await URLSession.shared.data(for: req),
              (resp as? HTTPURLResponse)?.statusCode == 200,
              let img = UIImage(data: data) else { return nil }
        cache.setObject(img, forKey: url as NSURL)
        return img
    }
}

struct AuthImage: View {
    let url: URL?
    let token: String
    var title: String = ""
    var year: Int? = nil
    /// Fit instead of fill, with no placeholder (title logos).
    var fit = false
    @State private var image: UIImage?

    var body: some View {
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
        .animation(.easeOut(duration: 0.35), value: image != nil)
        .task(id: url) {
            guard let url else { return }
            image = await ImageCache.shared.image(url: url, token: token)
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
                    Text(title).font(Theme.display(22)).foregroundStyle(.white.opacity(0.95))
                        .lineLimit(4).minimumScaleFactor(0.7)
                    if let year { Text(String(year)).font(.caption2).foregroundStyle(.white.opacity(0.5)) }
                }
                .padding(12)
            }
        }
    }
}
