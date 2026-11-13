import Foundation

// Books, comics, audiobooks and photos (api/openapi.yaml, tags books and photos).
// Same rules as Models.swift: required fields are non-optional, positions are milliseconds.

struct BookDetails: Codable, Sendable, Equatable {
    var format: String
    var authors: [String]
    var narrators: [String]?
    var seriesName: String?
    var seriesIndex: Double?
    var volume: Double?
    var chapter: Double?
    var pageCount: Int?
    var isbn: String?
    var publisher: String?
    var language: String?
    var published: String?
    var description: String?
    var rightToLeft: Bool?
}

struct PhotoDetails: Codable, Sendable, Equatable {
    var takenAt: String?
    var width: Int?
    var height: Int?
    var camera: String?
    var lens: String?
    var focalLength: Double?
    var fNumber: Double?
    var exposureTime: String?
    var iso: Int?
    var latitude: Double?
    var longitude: Double?
}

struct ReadingProgress: Codable, Sendable, Equatable {
    var page: Int?
    var locator: String?
    var progression: Double
    var finished: Bool
    var updatedAt: String?
}

struct ReadingProgressInput: Codable, Sendable, Equatable {
    var page: Int?
    var locator: String?
    var progression: Double
    var finished: Bool?
}

/// Everything a reader needs to open a book or comic; URLs are signed and server-relative.
struct ReadManifest: Codable, Sendable, Equatable {
    var format: String
    var fileUrl: String
    var pageCount: Int?
    var pages: [String]
    var rightToLeft: Bool
    var progress: ReadingProgress
}

struct AudiobookPart: Codable, Sendable, Equatable, Hashable {
    var fileId: String
    var title: String
    var offsetMs: Int64
    var durationMs: Int64
}

struct AudiobookChapter: Codable, Sendable, Equatable, Hashable {
    var title: String
    var startMs: Int64
    var endMs: Int64
}

/// Parts and chapters of an audiobook; every position is in the whole book.
struct AudiobookManifest: Codable, Sendable, Equatable {
    var durationMs: Int64
    var positionMs: Int64
    var parts: [AudiobookPart]
    var chapters: [AudiobookChapter]
}

/// A photo for the lightbox (the spec's PhotoView; renamed so it does not read like a SwiftUI view).
struct PhotoSource: Codable, Sendable, Equatable {
    var url: String
    var originalUrl: String?
    var mimeType: String
    var width: Int?
    var height: Int?
}

/// An author or series with how many titles it has.
struct NameCount: Codable, Sendable, Equatable, Hashable, Identifiable {
    var name: String
    var count: Int
    var itemId: String?

    var id: String { name }
}

extension MediaClient {
    func openBook(_ id: String) async throws -> ReadManifest { try await get("items/\(id)/read") }
    func readingProgress(_ id: String) async throws -> ReadingProgress { try await get("items/\(id)/reading-progress") }

    func saveReadingProgress(item id: String, _ input: ReadingProgressInput) async throws {
        try await send("PUT", "items/\(id)/reading-progress", input)
    }

    func openAudiobook(_ id: String) async throws -> AudiobookManifest { try await get("items/\(id)/listen") }
    func photo(_ id: String) async throws -> PhotoSource { try await get("items/\(id)/photo") }
    func authors(library id: String) async throws -> [NameCount] { try await get("libraries/\(id)/authors") }
    func series(library id: String) async throws -> [NameCount] { try await get("libraries/\(id)/series") }
    func continueReading() async throws -> [ItemSummary] { try await get("home/continue-reading") }
}
