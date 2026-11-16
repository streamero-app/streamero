import XCTest
@testable import StreameroManage

final class SchemaFieldDecodingTests: XCTestCase {
    private let schema = #"""
    [
      {"order":0,"name":"baseUrl","label":"URL","helpText":"Indexer URL","value":"https://x","type":"textbox","advanced":false,"privacy":"normal"},
      {"order":1,"name":"apiKey","label":"API Key","value":"********","type":"textbox","advanced":false,"privacy":"apiKey"},
      {"order":2,"name":"categories","label":"Categories","value":[2000,2010],"type":"select","advanced":false,
       "selectOptions":[{"value":2000,"name":"Movies","order":0},{"value":2010,"name":"Movies/Foreign","order":1,"hint":"(2010)"}]},
      {"order":3,"name":"minimumSeeders","label":"Minimum Seeders","value":1,"type":"number","advanced":true,"unit":"seeders"},
      {"order":4,"name":"seedRatio","label":"Seed Ratio","value":1.5,"type":"number","isFloat":true,"advanced":true},
      {"order":5,"name":"rssOnly","label":"RSS","value":false,"type":"checkbox"},
      {"order":6,"name":"futureThing","label":"Future","value":{"nested":[1,2]},"type":"hologram"},
      {"order":7,"name":"secretPw","label":"Password","value":"","type":"password"},
      {"order":8,"name":"internal","value":"x","type":"textbox","hidden":"hidden"},
      {"order":9,"name":"maybe","value":null,"type":"textbox","hidden":"hiddenIfNotSet"}
    ]
    """#

    private func fields() throws -> [ArrField] {
        try JSONValue.parse(Data(schema.utf8)).list.map(ArrField.init(json:))
    }

    func testDecodesKnownFieldTypes() throws {
        let f = try fields()
        XCTAssertEqual(f.count, 10)
        XCTAssertEqual(f[0].kind, .textbox)
        XCTAssertEqual(f[0].helpText, "Indexer URL")
        XCTAssertTrue(f[1].isSecret)
        XCTAssertEqual(f[2].kind, .select)
        XCTAssertTrue(f[2].isMultiSelect)
        XCTAssertEqual(f[2].options.map(\.name), ["Movies", "Movies/Foreign"])
        XCTAssertEqual(f[2].options[1].hint, "(2010)")
        XCTAssertEqual(f[3].kind, .number)
        XCTAssertTrue(f[3].advanced)
        XCTAssertEqual(f[3].unit, "seeders")
        XCTAssertTrue(f[4].isFloat)
        XCTAssertEqual(f[5].kind, .checkbox)
        XCTAssertTrue(f[7].isSecret)
    }

    func testUnknownTypesDecodeAndKeepTheirValue() throws {
        let f = try fields()
        XCTAssertEqual(f[6].kind, .unknown("hologram"))
        XCTAssertEqual(f[6].value["nested"].list.count, 2)
        // A missing label is derived from the name.
        XCTAssertEqual(f[8].label, "Internal")
    }

    func testHiddenFieldsAreNotShown() throws {
        let f = try fields()
        XCTAssertFalse(f[8].isVisible)
        XCTAssertFalse(f[9].isVisible)
        XCTAssertTrue(f[0].isVisible)
    }
}

final class SchemaFormRoundTripTests: XCTestCase {
    private func state() throws -> SchemaFormState {
        let text = #"[{"name":"baseUrl","label":"URL","value":"https://x","type":"textbox","order":0},{"name":"apiKey","label":"API Key","value":"********","type":"textbox","privacy":"apiKey"},{"name":"seeders","value":1,"type":"number"},{"name":"odd","value":{"k":1},"type":"hologram"}]"#
        return SchemaFormState(json: try JSONValue.parse(Data(text.utf8)))
    }

    func testUnchangedFormSendsBackWhatTheApiGave() throws {
        let s = try state()
        let out = s.outgoingFields().list
        XCTAssertEqual(out.count, 4)
        XCTAssertEqual(out[0]["value"].string, "https://x")
        XCTAssertEqual(out[1]["value"].string, "********")
        XCTAssertEqual(out[3]["value"]["k"].int, 1)
        // Other keys of each field survive (label, type, order).
        XCTAssertEqual(out[0]["order"].int, 0)
        XCTAssertTrue(s.changedNames.isEmpty)
    }

    func testSecretsAreWriteOnly() throws {
        var s = try state()
        // The UI never shows the stored (masked) value.
        XCTAssertEqual(s.secretText("apiKey"), "")
        s.setSecret("apiKey", "")
        XCTAssertEqual(s.outgoingFields()[1]["value"].string, "********")
        XCTAssertFalse(s.changedNames.contains("apiKey"))
        s.setSecret("apiKey", "new-key")
        XCTAssertEqual(s.outgoingFields()[1]["value"].string, "new-key")
        XCTAssertTrue(s.changedNames.contains("apiKey"))
    }

    func testEditedValuesAreSent() throws {
        var s = try state()
        s.set("baseUrl", "https://y")
        s.set("seeders", .int(5))
        let out = s.outgoingFields()
        XCTAssertEqual(out[0]["value"].string, "https://y")
        XCTAssertEqual(out[2]["value"].int, 5)
        XCTAssertEqual(s.changedNames, ["baseUrl", "seeders"])
    }

    func testObjectFormSendsOnlyChangesAndNoUnchangedSecrets() {
        let start: JSONValue = .object([
            "general": .object(["ip": .string("0.0.0.0"), "port": .int(6767), "enabled": .bool(true)]),
            "auth": .object(["apikey": .string("secret-key"), "password": .string("pw")]),
        ])
        var s = ObjectFormState(start)
        XCTAssertTrue(ObjectFormState.isSecretKey("auth.apikey"))
        XCTAssertFalse(ObjectFormState.isSecretKey("general.port"))
        XCTAssertTrue(s.changes().isEmpty)
        s.set("general.port", .int(7000))
        XCTAssertEqual(s.changes().map(\.path), ["general.port"])
        s.setSecret("auth.password", "new")
        let changes = s.changes()
        XCTAssertEqual(changes.map(\.path), ["general.port", "auth.password"])
        XCTAssertFalse(changes.contains { $0.path == "auth.apikey" })
        // The full object keeps unchanged secrets as the API gave them.
        XCTAssertEqual(s.fullObject().value(at: "auth.apikey").string, "secret-key")
        XCTAssertEqual(s.fullObject().value(at: "auth.password").string, "new")
    }

    func testBazarrFormFields() {
        let fields = BazarrSettings.formFields([
            JSONPathValue(path: "general.port", value: .int(7000)),
            JSONPathValue(path: "general.use_sonarr", value: .bool(true)),
            JSONPathValue(path: "general.enabled_providers", value: .array([.string("a"), .string("b")])),
        ])
        XCTAssertEqual(fields.map { $0.0 }, ["settings-general-port", "settings-general-use_sonarr",
                                         "settings-general-enabled_providers", "settings-general-enabled_providers"])
        XCTAssertEqual(fields.map { $0.1 }, ["7000", "true", "a", "b"])
    }
}

final class QualityProfileTests: XCTestCase {
    /// API order: lowest first. SD, a "WEB 1080p" group, Bluray-1080p, Remux-2160p.
    private var profile: JSONValue {
        let text = #"""
        {"id":4,"name":"HD","upgradeAllowed":true,"cutoff":7,
         "items":[
          {"quality":{"id":1,"name":"SDTV"},"items":[],"allowed":false},
          {"id":1001,"name":"WEB 1080p","items":[
            {"quality":{"id":3,"name":"WEBDL-1080p"},"items":[],"allowed":true},
            {"quality":{"id":15,"name":"WEBRip-1080p"},"items":[],"allowed":true}],"allowed":true},
          {"quality":{"id":7,"name":"Bluray-1080p"},"items":[],"allowed":true},
          {"quality":{"id":31,"name":"Remux-2160p"},"items":[],"allowed":false}],
         "formatItems":[{"format":9,"name":"HDR","score":10}],
         "minFormatScore":0,"cutoffFormatScore":100}
        """#
        return (try? JSONValue.parse(Data(text.utf8))) ?? .null
    }

    func testShowsHighestFirstAndReadsGroups() {
        let m = QualityProfileModel(json: profile)
        XCTAssertEqual(m.items.map(\.name), ["Remux-2160p", "Bluray-1080p", "WEB 1080p", "SDTV"])
        XCTAssertTrue(m.items[2].isGroup)
        XCTAssertEqual(m.items[2].id, 1001)
        XCTAssertEqual(m.items[2].members, ["WEBDL-1080p", "WEBRip-1080p"])
        XCTAssertEqual(m.allowedItems.map(\.id), [7, 1001])
    }

    func testReorderingIsSavedInApiOrder() {
        var m = QualityProfileModel(json: profile)
        // Move "WEB 1080p" (row 2) to the top.
        m.move(fromOffsets: IndexSet(integer: 2), toOffset: 0)
        XCTAssertEqual(m.items.map(\.name), ["WEB 1080p", "Remux-2160p", "Bluray-1080p", "SDTV"])
        let saved = m.json()["items"].list
        XCTAssertEqual(saved.map { $0["name"].string ?? $0["quality"]["name"].string ?? "" },
                       ["SDTV", "Bluray-1080p", "Remux-2160p", "WEB 1080p"])
        // Moving down past the moved row's own slot.
        m.move(fromOffsets: IndexSet(integer: 0), toOffset: 4)
        XCTAssertEqual(m.items.map(\.name), ["Remux-2160p", "Bluray-1080p", "SDTV", "WEB 1080p"])
    }

    func testCutoffFollowsAllowedQualities() {
        var m = QualityProfileModel(json: profile)
        m.setAllowed(7, false)
        XCTAssertEqual(m.cutoff, 1001, "the cutoff moves to the highest allowed quality")
        m.setAllowed(31, true)
        XCTAssertEqual(m.cutoff, 1001, "an allowed cutoff stays")
        let group = m.json()["items"].list.first { $0["id"].int == 1001 }
        XCTAssertEqual(group?["items"].list.map { $0["allowed"].bool }, [true, true])
    }

    func testDisallowingAGroupDisallowsItsQualities() {
        var m = QualityProfileModel(json: profile)
        m.setAllowed(1001, false)
        let group = m.json()["items"].list.first { $0["id"].int == 1001 }
        XCTAssertEqual(group?["allowed"].bool, false)
        XCTAssertEqual(group?["items"].list.map { $0["allowed"].bool }, [false, false])
    }

    func testFormatScoresAndUnknownKeysSurvive() {
        var m = QualityProfileModel(json: profile)
        m.setScore(format: 9, 50)
        m.cutoffFormatScore = 200
        let out = m.json()
        XCTAssertEqual(out["formatItems"][0]["score"].int, 50)
        XCTAssertEqual(out["cutoffFormatScore"].int, 200)
        XCTAssertEqual(out["id"].int, 4)
    }
}

final class AppIdentityTests: XCTestCase {
    func testManageBuildHasItsOwnSchemeAndStillReadsStandardInvites() {
        XCTAssertTrue(AppIdentity.isManage)
        XCTAssertEqual(AppIdentity.urlScheme, "streamero-manage")
        let url = URL(string: "streamero-manage://invite?server=https%3A%2F%2Fm.example&token=abcdefgh1234")!
        XCTAssertEqual(InviteLink.parse(url: url)?.token, "abcdefgh1234")
        XCTAssertEqual(InviteLink.parse("streamero://invite?token=abcdefgh1234")?.token, "abcdefgh1234")
    }
}
