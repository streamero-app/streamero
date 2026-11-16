import XCTest
@testable import StreameroManage

final class ArrProxyURLTests: XCTestCase {
    private let base = URL(string: "https://media.example.net/sub")!

    func testBuildsResourceURLUnderTheProxyAndApiBase() {
        let radarr = ArrClient(baseURL: base, token: "t", service: .radarr, apiBase: "api/v3")
        XCTAssertEqual(radarr.url("movie/12").absoluteString,
                       "https://media.example.net/sub/api/v1/arr/radarr/proxy/api/v3/movie/12")
        let bazarr = ArrClient(baseURL: base, token: "t", service: .bazarr, apiBase: "api")
        XCTAssertEqual(bazarr.url("system/status").absoluteString,
                       "https://media.example.net/sub/api/v1/arr/bazarr/proxy/api/system/status")
    }

    func testDefaultsTheApiBasePerService() {
        XCTAssertEqual(ArrClient(baseURL: base, token: "t", service: .sonarr).apiBase, "api/v3")
        XCTAssertEqual(ArrClient(baseURL: base, token: "t", service: .lidarr).apiBase, "api/v1")
        XCTAssertEqual(ArrClient(baseURL: base, token: "t", service: .prowlarr, apiBase: "/api/v1/").apiBase, "api/v1")
        XCTAssertEqual(ArrClient(baseURL: base, token: "t", service: .seerr, apiBase: "").apiBase, "api/v1")
    }

    func testAddsQueryItemsAndTrimsSlashes() {
        let sonarr = ArrClient(baseURL: base, token: "t", service: .sonarr)
        let url = sonarr.url("/episode/", query: [.q("seriesId", 3), .q("seasonNumber", 1)])
        XCTAssertEqual(url.absoluteString,
                       "https://media.example.net/sub/api/v1/arr/sonarr/proxy/api/v3/episode?seriesId=3&seasonNumber=1")
    }

    func testEncodesQueryValues() {
        let radarr = ArrClient(baseURL: base, token: "t", service: .radarr)
        let url = radarr.url("movie/lookup", query: [.q("term", "Alien & Čo+")])
        XCTAssertTrue(url.absoluteString.hasSuffix("movie/lookup?term=Alien%20%26%20%C4%8Co%2B"), url.absoluteString)
    }

    func testMediaCoverIsRelativeToTheProxyRoot() {
        let radarr = ArrClient(baseURL: base, token: "t", service: .radarr)
        XCTAssertEqual(radarr.mediaCoverURL("/radarr/MediaCover/7/poster.jpg?lastWrite=638")?.absoluteString,
                       "https://media.example.net/sub/api/v1/arr/radarr/proxy/MediaCover/7/poster.jpg?lastWrite=638")
        XCTAssertEqual(radarr.mediaCoverURL("/MediaCover/7/fanart.jpg")?.absoluteString,
                       "https://media.example.net/sub/api/v1/arr/radarr/proxy/MediaCover/7/fanart.jpg")
        XCTAssertNil(radarr.mediaCoverURL("https://image.tmdb.org/t/p/original/x.jpg"))
        XCTAssertNil(radarr.mediaCoverURL(nil))
    }

    func testFormBodyEncodesReservedCharacters() {
        let body = ArrClient.formBody([("settings-general-ip", "0.0.0.0"), ("a b", "x&y=z")])
        XCTAssertEqual(String(decoding: body, as: UTF8.self), "settings-general-ip=0.0.0.0&a%20b=x%26y%3Dz")
    }
}

final class ArrErrorMappingTests: XCTestCase {
    private func map(_ status: Int, _ json: String) -> ArrError {
        ArrError.from(status: status, data: Data(json.utf8))
    }

    func testMapsTheProxyErrorCodes() {
        XCTAssertEqual(map(401, #"{"code":"unauthenticated","message":"x"}"#), .unauthenticated)
        XCTAssertEqual(map(403, #"{"code":"forbidden","message":"x"}"#), .forbidden)
        XCTAssertEqual(map(409, #"{"code":"arr_not_configured","message":"x"}"#), .notConfigured)
        XCTAssertEqual(map(502, #"{"code":"arr_unreachable","message":"dial tcp"}"#), .unreachable("dial tcp"))
        XCTAssertEqual(map(400, #"{"code":"invalid_path","message":"x"}"#), .invalidPath)
        XCTAssertEqual(map(413, "too big"), .tooLarge)
    }

    func testReadsServarrValidationFailures() {
        let error = map(400, #"[{"propertyName":"BaseUrl","errorMessage":"Invalid URL","severity":"error"},{"propertyName":"","errorMessage":"Unable to connect","severity":"warning"}]"#)
        XCTAssertEqual(error.failures.count, 2)
        XCTAssertEqual(error.failures[0], ArrValidationFailure(propertyName: "BaseUrl", message: "Invalid URL", isWarning: false))
        XCTAssertTrue(error.failures[1].isWarning)
        XCTAssertTrue(error.failures[0].matches("baseUrl"))
        XCTAssertFalse(error.failures[0].matches("apiKey"))
    }

    func testKeepsTheServiceMessageForOtherErrors() {
        XCTAssertEqual(map(404, #"{"message":"NotFound"}"#), .http(404, "NotFound"))
        XCTAssertEqual(map(500, "<html>boom</html>"), .http(500, "<html>boom</html>"))
        XCTAssertEqual(map(400, #"{"message":"Bad title"}"#), .validation([ArrValidationFailure(propertyName: "", message: "Bad title", isWarning: false)]))
        XCTAssertEqual(map(503, ""), .http(503, nil))
    }

    func testUpstreamUnauthorizedWithoutCodeIsNotASessionProblem() {
        XCTAssertEqual(map(401, #"{"error":"Unauthorized"}"#), .http(401, "Unauthorized"))
    }
}

final class JSONValueTests: XCTestCase {
    func testRoundTripsAnyShape() throws {
        let text = #"{"a":1,"b":2.5,"c":"x","d":true,"e":null,"f":[1,"two",{"g":false}],"h":{"i":[]}}"#
        let value = try JSONValue.parse(Data(text.utf8))
        XCTAssertEqual(value["a"].int, 1)
        XCTAssertEqual(value["b"].double, 2.5)
        XCTAssertEqual(value["d"].bool, true)
        XCTAssertTrue(value["e"].isNull)
        XCTAssertEqual(value["f"][2]["g"].bool, false)
        let again = try JSONValue.parse(try value.encoded())
        XCTAssertEqual(again, value)
    }

    func testPathsAndFlattening() {
        var v: JSONValue = .object([
            "general": .object(["ip": .string("0.0.0.0"), "port": .int(6767)]),
            "auth": .object(["apikey": .string("k")]),
        ])
        v.setValue(.int(7000), at: "general.port")
        v.setValue(.string("new"), at: "sonarr.ip")
        XCTAssertEqual(v.value(at: "general.port").int, 7000)
        XCTAssertEqual(v.value(at: "sonarr.ip").string, "new")
        XCTAssertEqual(v.flattened().map(\.path), ["auth.apikey", "general.ip", "general.port", "sonarr.ip"])
    }

    func testHumanizesApiKeys() {
        XCTAssertEqual("enableAutomaticSearch".humanizedKey, "Enable Automatic Search")
        XCTAssertEqual("general.use_sonarr".humanizedKey, "Use sonarr")
        XCTAssertEqual("sslPort".humanizedKey, "Ssl Port")
    }
}
