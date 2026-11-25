import XCTest
@testable import Streamero

final class LibraryAccessTests: XCTestCase {
    private let scan = ScanStatus(state: "idle", lastScanAt: nil, error: nil, filesSeen: nil)
    private var libs: [Library] {
        [Library(id: "m", name: "Movies", kind: "movies", paths: [], itemCount: 0, scan: scan),
         Library(id: "x", name: "Private", kind: "adult", paths: [], itemCount: 0, scan: scan),
         Library(id: "s", name: "Shows", kind: "shows", paths: [], itemCount: 0, scan: scan)]
    }

    private func json(_ value: some Encodable) throws -> [String: Any] {
        try XCTUnwrap(JSONSerialization.jsonObject(with: MediaClient.encode(value)) as? [String: Any])
    }

    private let permissions = #""permissions":{"allLibraries":false,"download":true,"videoTranscoding":true,"audioTranscoding":true,"remux":true,"liveTv":false,"manageLiveTv":false,"syncPlay":false,"deleteMedia":false,"manageCollections":false,"editSubtitles":false,"remoteControl":false,"maxSessions":2}"#

    func testOlderServerWithoutTheFieldsDecodes() throws {
        let body = #"{"id":"u","username":"ana","isAdmin":false,"disabled":false,"totpEnabled":false,"#
            + permissions.replacingOccurrences(of: #""allLibraries":false"#, with: #""allLibraries":true"#) + #","createdAt":"x"}"#
        let u = try JSONDecoder().decode(User.self, from: Data(body.utf8))
        XCTAssertNil(u.libraryIds)
        XCTAssertEqual(LibraryAccess(u), LibraryAccess(all: true))

        let invite = try JSONDecoder().decode(Invite.self, from: Data(#"{"id":"i","note":"","createdBy":"a","createdAt":"x","expiresAt":"y","maxUses":1,"uses":0}"#.utf8))
        XCTAssertNil(invite.libraryIds)
    }

    func testDecodesAChosenList() throws {
        let body = #"{"id":"u","username":"ana","displayName":"Ana","isAdmin":false,"disabled":false,"totpEnabled":false,"#
            + permissions + #","libraryIds":["m","s"],"newLibraries":true,"createdAt":"x"}"#
        let u = try JSONDecoder().decode(User.self, from: Data(body.utf8))
        XCTAssertEqual(u.displayName, "Ana")
        XCTAssertEqual(LibraryAccess(u), LibraryAccess(all: false, ids: ["m", "s"], newLibraries: true))
    }

    func testThePickerLeavesOutAdultLibraries() {
        XCTAssertEqual(LibraryAccess.pickable(libs).map(\.id), ["m", "s"])
    }

    func testSavingAChosenListKeepsTheOtherPermissions() throws {
        var current = UserPermissions.newAccount
        current.maxSessions = 2
        // "x" is adult and "gone" no longer exists: neither is sent.
        let body = try json(LibraryAccess(all: false, ids: ["s", "x", "gone", "m"], newLibraries: true).update(keeping: current, libraries: libs))
        let perms = try XCTUnwrap(body["permissions"] as? [String: Any])
        XCTAssertEqual(perms["allLibraries"] as? Bool, false)
        XCTAssertEqual(perms["download"] as? Bool, true)
        XCTAssertEqual(perms["maxSessions"] as? Int, 2)
        XCTAssertEqual(body["libraryIds"] as? [String], ["m", "s"])
        XCTAssertEqual(body["newLibraries"] as? Bool, true)
        XCTAssertNil(body["isAdmin"])
    }

    func testSavingAllLibrariesOnlyFlipsThePermission() throws {
        var current = UserPermissions.newAccount
        current.allLibraries = false
        let body = try json(LibraryAccess(all: true, ids: ["m"]).update(keeping: current, libraries: libs))
        XCTAssertEqual((body["permissions"] as? [String: Any])?["allLibraries"] as? Bool, true)
        XCTAssertNil(body["libraryIds"])
        XCTAssertNil(body["newLibraries"])
    }

    func testInviteBodies() throws {
        let plain = try json(LibraryAccess().invite(expiresInHours: 72, maxUses: 1, libraries: libs))
        XCTAssertEqual(Set(plain.keys), ["expiresInHours", "maxUses"])

        let chosen = try json(LibraryAccess(all: false, ids: ["m", "x"]).invite(expiresInHours: 72, maxUses: 1, libraries: libs))
        let perms = try XCTUnwrap(chosen["permissions"] as? [String: Any])
        XCTAssertEqual(perms["allLibraries"] as? Bool, false)
        XCTAssertEqual(perms["liveTv"] as? Bool, true, "the rest matches what the server gives a new account")
        XCTAssertEqual(chosen["libraryIds"] as? [String], ["m"])
        XCTAssertEqual(chosen["newLibraries"] as? Bool, false)
    }
}
