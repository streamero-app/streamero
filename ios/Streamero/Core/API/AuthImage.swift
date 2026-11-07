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
    @State private var image: UIImage?

    var body: some View {
        ZStack {
            if let image {
                Image(uiImage: image).resizable().scaledToFill()
            } else {
                LinearGradient(colors: [Theme.surface2, Theme.surface], startPoint: .top, endPoint: .bottom)
                Text(title).font(.headline).multilineTextAlignment(.center).padding(8)
                    .foregroundStyle(Theme.textMuted)
            }
        }
        .task(id: url) {
            guard let url else { return }
            image = await ImageCache.shared.image(url: url, token: token)
        }
    }
}
