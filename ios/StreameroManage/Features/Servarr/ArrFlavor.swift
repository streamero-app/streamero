import Foundation

/// Where Radarr, Sonarr and Lidarr differ in resource names and commands; the screens are shared.
struct ArrFlavor: Sendable {
    let service: ArrService

    init(_ service: ArrService) { self.service = service }

    /// "movie", "series", "artist".
    var items: String {
        switch service {
        case .sonarr: "series"
        case .lidarr: "artist"
        default: "movie"
        }
    }

    var titleKey: String { service == .lidarr ? "artistName" : "title" }

    /// The query parameter naming one library item ("movieId").
    var idParam: String {
        switch service {
        case .sonarr: "seriesId"
        case .lidarr: "artistId"
        default: "movieId"
        }
    }

    var lookup: String { "\(items)/lookup" }

    var files: String {
        switch service {
        case .sonarr: "episodefile"
        case .lidarr: "trackfile"
        default: "moviefile"
        }
    }

    var history: String { "history/\(service == .sonarr ? "series" : service == .lidarr ? "artist" : "movie")" }

    /// Flags that embed the item in queue/history/wanted records.
    var includeQuery: [URLQueryItem] {
        switch service {
        case .sonarr: [.q("includeSeries", "true"), .q("includeEpisode", "true")]
        case .lidarr: [.q("includeArtist", "true"), .q("includeAlbum", "true")]
        default: [.q("includeMovie", "true")]
        }
    }

    func refresh(_ id: Int) -> (String, [String: JSONValue]) {
        switch service {
        case .sonarr: ("RefreshSeries", ["seriesId": .number(id)])
        case .lidarr: ("RefreshArtist", ["artistId": .number(id)])
        default: ("RefreshMovie", ["movieIds": .ints([id])])
        }
    }

    func search(_ id: Int) -> (String, [String: JSONValue]) {
        switch service {
        case .sonarr: ("SeriesSearch", ["seriesId": .number(id)])
        case .lidarr: ("ArtistSearch", ["artistId": .number(id)])
        default: ("MoviesSearch", ["movieIds": .ints([id])])
        }
    }

    /// Lidarr rescans folders per root folder, not per artist.
    func rescan(_ id: Int) -> (String, [String: JSONValue])? {
        switch service {
        case .sonarr: ("RescanSeries", ["seriesId": .number(id)])
        case .lidarr: nil
        default: ("RescanMovie", ["movieId": .number(id)])
        }
    }

    /// The search command for records of the wanted lists (movies, episodes, albums).
    func searchWanted(_ ids: [Int]) -> (String, [String: JSONValue]) {
        switch service {
        case .sonarr: ("EpisodeSearch", ["episodeIds": .ints(ids)])
        case .lidarr: ("AlbumSearch", ["albumIds": .ints(ids)])
        default: ("MoviesSearch", ["movieIds": .ints(ids)])
        }
    }

    func searchAll(cutoff: Bool) -> String {
        switch service {
        case .sonarr: cutoff ? "CutOffUnmetEpisodeSearch" : "MissingEpisodeSearch"
        case .lidarr: cutoff ? "CutoffUnmetAlbumSearch" : "MissingAlbumSearch"
        default: cutoff ? "CutoffUnmetMoviesSearch" : "MissingMoviesSearch"
        }
    }

    /// The import exclusion flag of the delete endpoint.
    var exclusionParam: String { service == .radarr ? "addImportExclusion" : "addImportListExclusion" }

    var coverType: String { "poster" }

    /// Whether everything monitored has a file.
    static func isComplete(_ item: JSONValue, service: ArrService) -> Bool {
        switch service {
        case .sonarr:
            let s = item["statistics"]
            return (s["episodeFileCount"].int ?? 0) >= (s["episodeCount"].int ?? 0)
        case .lidarr:
            let s = item["statistics"]
            return (s["trackFileCount"].int ?? 0) >= (s["totalTrackCount"].int ?? 0) && (s["totalTrackCount"].int ?? 0) > 0
        default:
            return item["hasFile"].bool ?? false
        }
    }

    /// "12/20" episodes or tracks, nil for movies.
    static func progress(_ item: JSONValue, service: ArrService) -> (have: Int, total: Int)? {
        let s = item["statistics"]
        switch service {
        case .sonarr: return (s["episodeFileCount"].int ?? 0, s["episodeCount"].int ?? 0)
        case .lidarr: return (s["trackFileCount"].int ?? 0, s["totalTrackCount"].int ?? 0)
        default: return nil
        }
    }

    static func sizeOnDisk(_ item: JSONValue) -> JSONValue {
        let direct = item["sizeOnDisk"]
        return direct.isNull ? item["statistics"]["sizeOnDisk"] : direct
    }
}
