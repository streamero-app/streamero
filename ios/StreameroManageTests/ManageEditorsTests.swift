import XCTest
@testable import StreameroManage

final class BazarrLanguageTests: XCTestCase {
    private func json(_ text: String) -> JSONValue { (try? JSONValue.parse(Data(text.utf8))) ?? .null }

    private var profile: BazarrLanguageProfile {
        BazarrLanguageProfile(json(#"""
        {"profileId":2,"name":"Main","cutoff":2,"originalFormat":false,"tag":null,"mustContain":["web"],"mustNotContain":[],
         "futureKey":"kept",
         "items":[{"id":1,"language":"en","forced":"False","hi":"False","audio_exclude":"False"},
                  {"id":2,"language":"hr","forced":"False","hi":"True","audio_exclude":"True"}]}
        """#))
    }

    func testReadsLanguagesAndFlags() {
        let langs = json(#"[{"name":"English","code2":"en","code3":"eng","enabled":true},{"name":"Croatian","code2":"hr","code3":"hrv","enabled":false}]"#)
            .list.map(BazarrLanguage.init(json:))
        XCTAssertEqual(BazarrLanguageSettings.enabledCodes(langs), ["en"])
        XCTAssertTrue(BazarrFlag.isOn(.string("True")))
        XCTAssertTrue(BazarrFlag.isOn(.bool(true)))
        XCTAssertFalse(BazarrFlag.isOn(.string("False")))
        XCTAssertFalse(BazarrFlag.isOn(.null))
    }

    func testFlagsKeepTheirShape() {
        XCTAssertEqual(BazarrFlag.value(true, like: .string("False")), .string("True"))
        XCTAssertEqual(BazarrFlag.value(true, like: .bool(false)), .bool(true))
        XCTAssertEqual(BazarrFlag.value(false, like: .null), .string("False"))
    }

    func testSubtitleKindEditsForcedAndHi() {
        var item = profile.items[1]
        XCTAssertEqual(BazarrLanguageProfile.kind(of: item), .hearingImpaired)
        BazarrLanguageProfile.setKind(.forced, on: &item)
        XCTAssertEqual(item["forced"], .string("True"))
        XCTAssertEqual(item["hi"], .string("False"))
        XCTAssertEqual(BazarrLanguageProfile.kind(of: item), .forced)
    }

    func testAddRemoveAndReorderItems() {
        var p = profile
        p.addItem(language: "de")
        XCTAssertEqual(p.items.count, 3)
        XCTAssertEqual(p.items[2]["id"].int, 3)
        XCTAssertEqual(p.items[2]["language"].string, "de")
        XCTAssertEqual(p.items[2]["hi"], .string("False"))
        XCTAssertEqual(p.items[2]["audio_exclude"], .string("False"))

        p.moveItems(from: IndexSet(integer: 2), to: 0)
        XCTAssertEqual(p.items.map { $0["language"].text }, ["de", "en", "hr"])

        // Removing the cutoff item clears the cutoff.
        p.removeItems(at: IndexSet(integer: 2))
        XCTAssertNil(p.cutoff)
        XCTAssertEqual(p.items.map { $0["language"].text }, ["de", "en"])
    }

    func testNewProfileGetsTheNextId() {
        let p = BazarrLanguageProfile.new(name: "Kids", after: [profile])
        XCTAssertEqual(p.id, 3)
        XCTAssertEqual(p.name, "Kids")
        XCTAssertTrue(p.items.isEmpty)
    }

    func testFormEncodingOfLanguagesAndProfiles() throws {
        var p = profile
        p.name = "Main & more"
        let fields = BazarrLanguageSettings.formFields(enabled: ["en", "hr"], profiles: [p])
        XCTAssertEqual(fields.map { $0.0 }, ["languages-enabled", "languages-enabled", "languages-profiles"])
        XCTAssertEqual(fields[0].1, "en")
        XCTAssertEqual(fields[1].1, "hr")
        // The profiles go as one JSON text with unknown keys intact.
        let decoded = try JSONValue.parse(Data(fields[2].1.utf8))
        XCTAssertEqual(decoded[0]["name"].string, "Main & more")
        XCTAssertEqual(decoded[0]["futureKey"].string, "kept")
        XCTAssertEqual(decoded[0]["items"].list.count, 2)

        // Percent-encoded as a form body: "&" and the JSON punctuation are escaped.
        let body = String(decoding: ArrClient.formBody(fields), as: UTF8.self)
        XCTAssertTrue(body.hasPrefix("languages-enabled=en&languages-enabled=hr&languages-profiles=%5B%7B"))
        XCTAssertFalse(body.contains("Main & more"))
        XCTAssertTrue(body.contains("Main%20%26%20more"))
    }

    func testOnlyChangedPartsAreSent() {
        XCTAssertTrue(BazarrLanguageSettings.formFields(enabled: nil, profiles: nil).isEmpty)
        XCTAssertEqual(BazarrLanguageSettings.formFields(enabled: nil, profiles: []).map { $0.1 }, ["[]"])
    }
}

final class SeerrPermissionTests: XCTestCase {
    func testBitsMapToLabelledPermissions() {
        let mask: Int64 = 2 | 32 | 1_024
        XCTAssertEqual(SeerrPermissions.enabled(mask).map(\.key), ["manage.perm.admin", "manage.perm.request", "manage.perm.request_4k"])
        XCTAssertEqual(SeerrPermissions.admin.position, 1)
        // Every documented bit has one label.
        XCTAssertEqual(Set(SeerrPermissions.known.map(\.bit)).count, SeerrPermissions.known.count)
    }

    func testRoundTripKeepsUnknownBits() {
        let unknown: Int64 = (1 << 28) | (1 << 40)
        for mask: Int64 in [0, 2, 32 | 262_144, 0x0FFF_FFFE, unknown | 16 | 4_194_304] {
            let rebuilt = SeerrPermissions.mask(enabled: SeerrPermissions.enabled(mask), keepingUnknownFrom: mask)
            XCTAssertEqual(rebuilt, mask)
        }
        XCTAssertEqual(SeerrPermissions.unknownPositions(unknown | 32), [28, 40])
    }

    func testTogglingChangesOnlyThatBit() {
        let start: Int64 = (1 << 28) | 32
        let request4k = SeerrPermissions.known.first { $0.key == "manage.perm.request_4k" }!
        let on = SeerrPermissions.setting(start, request4k, true)
        XCTAssertEqual(on, start | 1_024)
        XCTAssertEqual(SeerrPermissions.setting(on, request4k, false), start)
    }

    func testDetectsRemovingAdmin() {
        XCTAssertTrue(SeerrPermissions.removesAdmin(from: 2 | 32, to: 32))
        XCTAssertFalse(SeerrPermissions.removesAdmin(from: 32, to: 32))
        XCTAssertFalse(SeerrPermissions.removesAdmin(from: 2, to: 2 | 4))
    }

    func testQuotaKeepsOtherSettings() {
        let main: JSONValue = ["username": "ana", "locale": "hr", "movieQuotaLimit": 5, "movieQuotaDays": 7, "tvQuotaLimit": .null, "tvQuotaDays": .null]
        XCTAssertTrue(SeerrQuota.isPresent(in: main))
        var q = SeerrQuota(main)
        XCTAssertEqual(q.movieLimit, 5)
        XCTAssertNil(q.tvLimit)
        q.tvLimit = 2
        q.movieLimit = nil
        let out = q.applied(to: main)
        XCTAssertEqual(out["username"].string, "ana")
        XCTAssertEqual(out["tvQuotaLimit"].int, 2)
        XCTAssertTrue(out["movieQuotaLimit"].isNull)
        XCTAssertFalse(SeerrQuota.isPresent(in: ["username": "x"]))
    }
}

final class BulkEditAndServerTests: XCTestCase {
    func testBulkEditSendsOnlyWhatIsSet() {
        var edit = ArrBulkEdit()
        XCTAssertTrue(edit.isEmpty)
        edit.monitored = false
        edit.rootFolderPath = "/media/movies"
        edit.moveFiles = true
        edit.tags = [3, 4]
        edit.tagMode = .remove
        edit.seriesType = "anime" // Sonarr only: dropped for Radarr.
        let body = edit.body(service: .radarr, ids: [1, 2])
        XCTAssertEqual(body["movieIds"], .ints([1, 2]))
        XCTAssertEqual(body["monitored"], .bool(false))
        XCTAssertEqual(body["rootFolderPath"].string, "/media/movies")
        XCTAssertEqual(body["moveFiles"], .bool(true))
        XCTAssertEqual(body["tags"], .ints([3, 4]))
        XCTAssertEqual(body["applyTags"].string, "remove")
        XCTAssertTrue(body["seriesType"].isNull)
        XCTAssertTrue(body["qualityProfileId"].isNull)
    }

    func testBulkIdsKeyPerService() {
        XCTAssertEqual(ArrBulkEdit().body(service: .sonarr, ids: [9])["seriesIds"], .ints([9]))
        XCTAssertEqual(ArrBulkEdit().body(service: .lidarr, ids: [9])["artistIds"], .ints([9]))
        let del = ArrBulkEdit.deleteBody(service: .sonarr, ids: [1], deleteFiles: true, addExclusion: false)
        XCTAssertEqual(del["seriesIds"], .ints([1]))
        XCTAssertEqual(del["deleteFiles"], .bool(true))
        XCTAssertEqual(del["addImportListExclusion"], .bool(false))
        XCTAssertEqual(ArrBulkEdit.deleteBody(service: .radarr, ids: [1], deleteFiles: false, addExclusion: true)["addImportExclusion"], .bool(true))
    }

    func testServerTestFillsPickersAndProfileNames() throws {
        let answer = try JSONValue.parse(Data(#"""
        {"profiles":[{"id":1,"name":"Any"},{"id":6,"name":"HD-1080p"}],
         "rootFolders":[{"id":1,"path":"/tv"},{"id":2,"path":"/anime"}],
         "languageProfiles":[{"id":1,"name":"English"}],
         "tags":[{"id":4,"label":"kids"}]}
        """#.utf8))
        let test = SeerrServerTest(answer)
        XCTAssertEqual(test.options["activeProfileId"]?.map(\.name), ["Any", "HD-1080p"])
        XCTAssertEqual(test.options["activeDirectory"]?.map(\.value), [.string("/tv"), .string("/anime")])
        XCTAssertEqual(test.options["activeLanguageProfileId"]?.count, 1)
        XCTAssertEqual(test.options["animeTags"]?.first?.name, "kids")

        var server = SeerrServerForm.template(.sonarr)
        server["activeProfileId"] = 6
        let synced = test.syncingNames(server)
        XCTAssertEqual(synced["activeProfileName"].string, "HD-1080p")
        XCTAssertEqual(synced["hostname"].string, "")
    }

    func testRadarrTestWithoutLanguageProfiles() {
        let test = SeerrServerTest(["profiles": [["id": 1, "name": "Any"]], "rootFolders": [], "tags": []])
        XCTAssertNil(test.options["activeLanguageProfileId"])
        XCTAssertNil(test.options["activeDirectory"])
        XCTAssertEqual(SeerrServerForm.template(.radarr)["minimumAvailability"].string, "released")
    }
}

final class JSONArrayEditingTests: XCTestCase {
    func testNewElementCopiesTheShapeWithoutValues() {
        let items: [JSONValue] = [["id": 3, "name": "a", "enabled": true, "ports": [1, 2], "nested": ["k": "v"]]]
        let new = JSONArrayTemplate.newElement(for: items)
        XCTAssertTrue(new["id"].isNull)
        XCTAssertEqual(new["name"], .string(""))
        XCTAssertEqual(new["enabled"], .bool(false))
        XCTAssertEqual(new["ports"], .array([]))
        XCTAssertEqual(new["nested"]["k"], .string(""))
        XCTAssertEqual(JSONArrayTemplate.newElement(for: [.int(5)]), .int(0))
        XCTAssertEqual(JSONArrayTemplate.newElement(for: []), .string(""))
    }

    func testMovingElements() {
        let list: [JSONValue] = [1, 2, 3, 4]
        XCTAssertEqual(JSONArrayTemplate.moving(list, from: IndexSet(integer: 0), to: 4), [2, 3, 4, 1])
        XCTAssertEqual(JSONArrayTemplate.moving(list, from: IndexSet(integer: 3), to: 0), [4, 1, 2, 3])
        XCTAssertEqual(JSONArrayTemplate.label(["name": "x"], index: 0), "x")
        XCTAssertEqual(JSONArrayTemplate.label(["port": 1], index: 1), "#2")
    }
}
