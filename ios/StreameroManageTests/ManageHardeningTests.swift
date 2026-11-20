import XCTest
@testable import StreameroManage

final class ManageErrorMappingTests: XCTestCase {
    private func map(_ status: Int, _ body: String) -> ArrError {
        ArrError.from(status: status, data: Data(body.utf8))
    }

    func testMapsTheNewerProxyCodes() {
        XCTAssertEqual(map(403, #"{"code":"read_only_key","message":"read-only API key"}"#), .readOnlyKey)
        XCTAssertEqual(map(429, #"{"code":"rate_limited","message":"slow down"}"#), .rateLimited)
        XCTAssertEqual(map(405, #"{"code":"method_not_allowed","message":"x"}"#), .methodNotAllowed)
        XCTAssertEqual(map(413, #"{"code":"request_too_large","message":"x"}"#), .tooLarge)
    }

    func testStatusAloneStillTellsTooManyRequests() {
        XCTAssertEqual(map(429, "<html>Too Many Requests</html>"), .rateLimited)
    }

    func testAPlainForbiddenFromTheServiceIsNotAReadOnlyKey() {
        XCTAssertEqual(map(403, #"{"message":"Forbidden"}"#), .http(403, "Forbidden"))
    }
}

final class NumberFieldTests: XCTestCase {
    func testHugeIntegersDoNotCrash() {
        // Used to trap in Int64(Double) for anything beyond Int64.
        XCTAssertEqual(JSONNumberField.parse("99999999999999999999", isFloat: false), .double(1e20))
        XCTAssertEqual(JSONNumberField.parse("1e19", isFloat: false), .double(1e19))
        XCTAssertNil(JSONNumberField.parse("inf", isFloat: false))
        XCTAssertNil(JSONNumberField.parse("nan", isFloat: true))
    }

    func testOrdinaryNumbers() {
        XCTAssertEqual(JSONNumberField.parse("42", isFloat: false), .int(42))
        XCTAssertEqual(JSONNumberField.parse("3,5", isFloat: true), .double(3.5))
        XCTAssertEqual(JSONNumberField.parse("2.0", isFloat: false), .int(2))
        XCTAssertNil(JSONNumberField.parse("abc", isFloat: false))
    }

    func testClearingKeepsTheTypeUnlessNullIsAllowed() {
        XCTAssertNil(JSONNumberField.update(text: "", current: .int(7878), isFloat: false, allowsNull: false),
                     "an emptied port keeps its number instead of becoming null (and then a string)")
        XCTAssertEqual(JSONNumberField.update(text: "", current: .int(5), isFloat: false, allowsNull: true), .null)
        XCTAssertEqual(JSONNumberField.update(text: "8989", current: .int(7878), isFloat: false, allowsNull: false), .int(8989))
        XCTAssertNil(JSONNumberField.update(text: "7878", current: .int(7878), isFloat: false, allowsNull: false))
    }

    func testByteTextNeverTraps() {
        XCTAssertEqual(byteText(.double(1e30)), "")
        XCTAssertFalse(byteText(.int(1024)).isEmpty)
    }
}

final class SecretKeyTests: XCTestCase {
    func testNotificationAgentSecretsAreWriteOnly() {
        for key in ["options.authPass", "options.pgpPrivateKey", "options.botAPI", "options.authHeader",
                    "options.webhookUrl", "options.accessToken", "apiKey", "password"] {
            XCTAssertTrue(ObjectFormState.isSecretKey(key), key)
        }
        for key in ["options.emailFrom", "port", "options.bypass", "hostname"] {
            XCTAssertFalse(ObjectFormState.isSecretKey(key), key)
        }
    }
}

final class JSONParsingTests: XCTestCase {
    func testKeepsBooleansIntegersAndDoubles() throws {
        let v = try JSONValue.parse(Data(#"{"b":true,"z":0,"i":9007199254740993,"d":1.5,"n":null,"s":"x"}"#.utf8))
        XCTAssertEqual(v["b"], .bool(true))
        XCTAssertEqual(v["z"], .int(0), "0 is a number, not false")
        XCTAssertEqual(v["i"], .int(9_007_199_254_740_993), "large ids keep every digit")
        XCTAssertEqual(v["d"], .double(1.5))
        XCTAssertTrue(v["n"].isNull)
        XCTAssertEqual(v["s"], .string("x"))
    }

    func testEmptyBodyIsNullAndGarbageThrows() {
        XCTAssertEqual(try JSONValue.parse(Data()), .null)
        XCTAssertThrowsError(try JSONValue.parse(Data("<html>".utf8)))
    }
}

final class QualityProfileRoundTripTests: XCTestCase {
    func testFormatItemsKeepUnknownFields() {
        let profile: JSONValue = .object([
            "id": .int(4), "name": .string("HD"), "upgradeAllowed": .bool(true), "cutoff": .int(1), "items": .array([]),
            "minFormatScore": .int(0), "cutoffFormatScore": .int(0),
            "formatItems": .array([.object(["format": .int(9), "name": .string("x265"), "score": .int(10), "future": .string("keep")])]),
        ])
        var m = QualityProfileModel(json: profile)
        m.setScore(format: 9, 50)
        let item = m.json()["formatItems"][0]
        XCTAssertEqual(item["score"].int, 50)
        XCTAssertEqual(item["future"].string, "keep")
        XCTAssertEqual(item["name"].string, "x265")
    }
}
