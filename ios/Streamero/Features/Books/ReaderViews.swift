import PDFKit
import SwiftUI
import UIKit

/// Opens a book's reader manifest and shows the right reader: comics page by page, PDFs with
/// PDFKit. EPUB and MOBI are read in the web app.
struct ReaderScreen: View {
    let target: ReaderTarget
    let client: MediaClient
    @Environment(\.dismiss) private var dismiss
    @State private var manifest: ReadManifest?
    @State private var error: Error?

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            if let m = manifest {
                if !m.pages.isEmpty {
                    ComicReaderView(target: target, manifest: m, client: client)
                } else if BookFormat.isPDF(m.format) {
                    PDFReaderView(target: target, manifest: m, client: client)
                } else {
                    message(Text("books.readInWeb"))
                }
            } else if let error {
                message(errorMessage(error))
            } else {
                ProgressView().tint(.white)
            }
        }
        .task {
            do {
                manifest = try await client.openBook(target.id)
            } catch is CancellationError {
                return
            } catch {
                self.error = error
            }
        }
    }

    private func message(_ text: Text) -> some View {
        VStack(spacing: 18) {
            text.multilineTextAlignment(.center).foregroundStyle(Theme.textMuted)
            Button { dismiss() } label: { Text("close") }.buttonStyle(MutedButtonStyle())
        }
        .padding(32)
    }
}

/// The reader's top bar: close, title and optional trailing controls.
struct ReaderTopBar<Trailing: View>: View {
    let title: String
    @ViewBuilder let trailing: () -> Trailing
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        HStack(spacing: 12) {
            Button { dismiss() } label: {
                Image(systemName: "xmark").font(.body.weight(.semibold)).frame(width: 40, height: 40)
            }
            .accessibilityLabel(Text("close"))
            Text(verbatim: title).font(.subheadline.weight(.semibold)).lineLimit(1)
            Spacer(minLength: 0)
            trailing()
        }
        .foregroundStyle(Theme.text)
        .padding(.horizontal, 12).padding(.vertical, 6)
        .background(.black.opacity(0.7))
    }
}

// MARK: Comics

/// Page-by-page comic reader: swipe or tap the sides to turn, tap the middle for the controls,
/// pinch or double-tap to zoom. Right-to-left follows the book and can be switched.
struct ComicReaderView: View {
    let target: ReaderTarget
    let manifest: ReadManifest
    let client: MediaClient
    @State private var page = 0
    @State private var savedPage: Int?
    @State private var rightToLeft = false
    @State private var chrome = true
    @State private var started = false

    private var count: Int { manifest.pages.count }
    private var paging: ComicPaging { ComicPaging(pageCount: count, rightToLeft: rightToLeft) }

    var body: some View {
        TabView(selection: Binding(get: { paging.slot(forPage: page) }, set: { page = paging.page(forSlot: $0) })) {
            ForEach(0..<count, id: \.self) { slot in
                ZoomableImage(url: client.absolute(manifest.pages[paging.page(forSlot: slot)]), token: client.token) { location, size in
                    tap(x: location.x, width: size.width)
                }
                .tag(slot)
            }
        }
        .tabViewStyle(.page(indexDisplayMode: .never))
        .id(rightToLeft)
        .background(Color.black)
        .ignoresSafeArea()
        .overlay(alignment: .top) {
            if chrome {
                ReaderTopBar(title: target.title) {
                    Button { rightToLeft.toggle() } label: {
                        Label(rightToLeft ? "books.rightToLeft" : "books.leftToRight",
                              systemImage: rightToLeft ? "arrow.left" : "arrow.right")
                            .font(.footnote.weight(.semibold))
                    }
                }
                .transition(.move(edge: .top).combined(with: .opacity))
            }
        }
        .overlay(alignment: .bottom) {
            if chrome && count > 0 { bottomBar.transition(.move(edge: .bottom).combined(with: .opacity)) }
        }
        .statusBarHidden(!chrome)
        .persistentSystemOverlays(chrome ? .automatic : .hidden)
        .onAppear {
            guard !started else { return }
            started = true
            rightToLeft = manifest.rightToLeft
            page = ReadingMath.resumePage(manifest.progress, pageCount: count)
            savedPage = page
        }
        .task(id: page) {
            await prefetch(after: page)
            // Saves once the page has stayed for a moment.
            try? await Task.sleep(for: .seconds(2))
            if !Task.isCancelled { save() }
        }
        .onDisappear { save() }
    }

    private var bottomBar: some View {
        VStack(spacing: 8) {
            if count > 1 {
                Slider(value: Binding(get: { Double(page) }, set: { page = min(count - 1, max(0, Int($0.rounded()))) }),
                       in: 0...Double(count - 1), step: 1)
                    .tint(Theme.accent)
                    .environment(\.layoutDirection, rightToLeft ? .rightToLeft : .leftToRight)
            }
            Text("books.pageOf \(page + 1) \(count)").font(.caption.monospacedDigit()).foregroundStyle(Theme.textMuted)
        }
        .padding(.horizontal, 20).padding(.top, 12).padding(.bottom, 28)
        .background(.black.opacity(0.7))
    }

    private func tap(x: CGFloat, width: CGFloat) {
        let zone = ComicPaging.zone(x: Double(x), width: Double(width))
        if let next = paging.page(after: zone, from: page) {
            withAnimation(.easeOut(duration: 0.25)) { page = next }
        } else if zone == .center {
            withAnimation(.easeOut(duration: 0.2)) { chrome.toggle() }
        }
    }

    /// Warms the cache with the next two pages.
    private func prefetch(after page: Int) async {
        for p in (page + 1)...(page + 2) where manifest.pages.indices.contains(p) {
            guard !Task.isCancelled, let url = client.absolute(manifest.pages[p]) else { return }
            _ = await ImageCache.shared.image(url: url, token: client.token)
        }
    }

    private func save() {
        guard started, count > 0, page != savedPage else { return }
        savedPage = page
        let input = ReadingMath.input(page: page, pageCount: count)
        Task { [client, id = target.id] in try? await client.saveReadingProgress(item: id, input) }
    }
}

/// A picture that fits the screen, with pinch and double-tap zoom and a drag while zoomed.
/// `onTap` gets single taps (location and size) once a double tap is ruled out.
struct ZoomableImage: View {
    let url: URL?
    let token: String
    var onTap: ((CGPoint, CGSize) -> Void)? = nil
    @State private var scale: CGFloat = 1
    @State private var baseScale: CGFloat = 1
    @State private var offset: CGSize = .zero
    @State private var baseOffset: CGSize = .zero

    var body: some View {
        GeometryReader { geo in
            ZStack {
                ProgressView().tint(.white)
                AuthImage(url: url, token: token, fit: true)
            }
            .frame(width: geo.size.width, height: geo.size.height)
            .scaleEffect(scale)
            .offset(offset)
            .contentShape(Rectangle())
            .gesture(taps(size: geo.size))
            .simultaneousGesture(magnify)
            .simultaneousGesture(pan, including: scale > 1 ? .all : .none)
        }
        .clipped()
        .onChange(of: url) { _, _ in reset() }
    }

    private func taps(size: CGSize) -> some Gesture {
        TapGesture(count: 2)
            .onEnded {
                withAnimation(.spring(response: 0.3, dampingFraction: 0.85)) {
                    if scale > 1 {
                        reset()
                    } else {
                        scale = 2.5
                        baseScale = 2.5
                    }
                }
            }
            .exclusively(before: SpatialTapGesture().onEnded { value in onTap?(value.location, size) })
    }

    private var magnify: some Gesture {
        MagnifyGesture()
            .onChanged { value in scale = min(5, max(1, baseScale * value.magnification)) }
            .onEnded { _ in
                baseScale = scale
                if scale <= 1.01 { withAnimation(.easeOut(duration: 0.2)) { reset() } }
            }
    }

    private var pan: some Gesture {
        DragGesture()
            .onChanged { value in
                offset = CGSize(width: baseOffset.width + value.translation.width, height: baseOffset.height + value.translation.height)
            }
            .onEnded { _ in baseOffset = offset }
    }

    private func reset() {
        scale = 1
        baseScale = 1
        offset = .zero
        baseOffset = .zero
    }
}

// MARK: PDF

/// Downloaded book files, kept in the caches folder so reopening is instant.
enum BookFileCache {
    static func file(for id: String, ext: String, from url: URL) async throws -> URL {
        let fm = FileManager.default
        let dir = try fm.url(for: .cachesDirectory, in: .userDomainMask, appropriateFor: nil, create: true)
            .appending(path: "books", directoryHint: .isDirectory)
        try fm.createDirectory(at: dir, withIntermediateDirectories: true)
        let dest = dir.appending(path: "\(id).\(ext)")
        if fm.fileExists(atPath: dest.path(percentEncoded: false)) { return dest }
        let (tmp, response) = try await URLSession.shared.download(from: url)
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard status == 200 else {
            try? fm.removeItem(at: tmp)
            throw APIError(status: status, code: "download_failed")
        }
        try? fm.removeItem(at: dest)
        try fm.moveItem(at: tmp, to: dest)
        return dest
    }
}

struct PDFReaderView: View {
    let target: ReaderTarget
    let manifest: ReadManifest
    let client: MediaClient
    @State private var document: PDFDocument?
    @State private var failed = false
    @State private var page = 0
    @State private var savedPage: Int?
    @State private var startPage = 0
    @State private var pageCount = 0

    var body: some View {
        VStack(spacing: 0) {
            ReaderTopBar(title: target.title) {
                if pageCount > 0 {
                    Text("books.pageOf \(page + 1) \(pageCount)").font(.caption.monospacedDigit()).foregroundStyle(Theme.textMuted)
                }
            }
            ZStack {
                Color.black
                if let document {
                    PDFKitView(document: document, startPage: startPage) { page = $0 }
                } else if failed {
                    Text("books.openFailed").foregroundStyle(Theme.textMuted).padding(32)
                } else {
                    ProgressView().tint(.white)
                }
            }
            .ignoresSafeArea(edges: .bottom)
        }
        .background(Color.black.ignoresSafeArea())
        .task { await open() }
        .task(id: page) {
            try? await Task.sleep(for: .seconds(2))
            if !Task.isCancelled { save() }
        }
        .onDisappear { save() }
    }

    private func open() async {
        guard document == nil else { return }
        guard let url = client.absolute(manifest.fileUrl) else {
            failed = true
            return
        }
        do {
            let file = try await BookFileCache.file(for: target.id, ext: "pdf", from: url)
            guard let doc = PDFDocument(url: file) else {
                failed = true
                return
            }
            pageCount = doc.pageCount
            startPage = ReadingMath.resumePage(manifest.progress, pageCount: doc.pageCount)
            page = startPage
            savedPage = startPage
            document = doc
        } catch {
            failed = true
        }
    }

    private func save() {
        guard pageCount > 0, savedPage != nil, page != savedPage else { return }
        savedPage = page
        let input = ReadingMath.input(page: page, pageCount: pageCount)
        Task { [client, id = target.id] in try? await client.saveReadingProgress(item: id, input) }
    }
}

/// PDFKit's view; reports the page on screen.
struct PDFKitView: UIViewRepresentable {
    let document: PDFDocument
    let startPage: Int
    let onPageChange: (Int) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(onPageChange: onPageChange) }

    func makeUIView(context: Context) -> PDFView {
        let view = PDFView()
        view.document = document
        view.autoScales = true
        view.displayMode = .singlePageContinuous
        view.displayDirection = .vertical
        view.backgroundColor = .black
        NotificationCenter.default.addObserver(context.coordinator, selector: #selector(Coordinator.pageChanged(_:)),
                                               name: .PDFViewPageChanged, object: view)
        // Going to a page needs the view laid out first.
        let start = startPage
        Task { @MainActor [weak view] in
            guard let view, start > 0, let page = view.document?.page(at: start) else { return }
            view.go(to: page)
        }
        return view
    }

    func updateUIView(_ view: PDFView, context: Context) {
        context.coordinator.onPageChange = onPageChange
    }

    static func dismantleUIView(_ view: PDFView, coordinator: Coordinator) {
        NotificationCenter.default.removeObserver(coordinator)
    }

    @MainActor
    final class Coordinator: NSObject {
        var onPageChange: (Int) -> Void

        init(onPageChange: @escaping (Int) -> Void) {
            self.onPageChange = onPageChange
        }

        @objc func pageChanged(_ note: Notification) {
            guard let view = note.object as? PDFView, let page = view.currentPage, let doc = view.document else { return }
            onPageChange(doc.index(for: page))
        }
    }
}
