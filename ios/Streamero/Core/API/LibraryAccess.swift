import Foundation

/// Which libraries a non-admin account sees (D-054): every library, or a chosen list.
/// Adult libraries are never part of the list; they have their own grant (D-053).
struct LibraryAccess: Equatable, Sendable {
    var all = true
    var ids: Set<String> = []
    var newLibraries = false

    init(all: Bool = true, ids: Set<String> = [], newLibraries: Bool = false) {
        self.all = all
        self.ids = ids
        self.newLibraries = newLibraries
    }

    init(_ user: User) {
        self.init(all: user.permissions.allLibraries, ids: Set(user.libraryIds ?? []), newLibraries: user.newLibraries ?? false)
    }

    /// Libraries an admin can put on a chosen list: everything except adult ones.
    static func pickable(_ libraries: [Library]) -> [Library] { libraries.filter { !$0.isAdult } }

    mutating func set(_ id: String, _ on: Bool) {
        if on { ids.insert(id) } else { ids.remove(id) }
    }

    /// PATCH /users/{id} body; `current` keeps the user's other permissions as they are.
    func update(keeping current: UserPermissions, libraries: [Library]) -> UpdateUser {
        var perms = current
        perms.allLibraries = all
        if all { return UpdateUser(permissions: perms) }
        return UpdateUser(permissions: perms, libraryIds: chosen(libraries), newLibraries: newLibraries)
    }

    /// POST /invites body; an all-libraries invite leaves permissions to the server default.
    func invite(expiresInHours: Int, maxUses: Int, libraries: [Library]) -> CreateInvite {
        if all { return CreateInvite(expiresInHours: expiresInHours, maxUses: maxUses) }
        var perms = UserPermissions.newAccount
        perms.allLibraries = false
        return CreateInvite(expiresInHours: expiresInHours, maxUses: maxUses, permissions: perms,
                            libraryIds: chosen(libraries), newLibraries: newLibraries)
    }

    /// Chosen ids that still exist and are pickable, in library order.
    private func chosen(_ libraries: [Library]) -> [String] {
        Self.pickable(libraries).map(\.id).filter { ids.contains($0) }
    }
}

extension UserPermissions {
    /// What the server gives a new account when nothing is chosen.
    static let newAccount = UserPermissions(
        allLibraries: true, download: true, videoTranscoding: true, audioTranscoding: true, remux: true,
        liveTv: true, manageLiveTv: false, syncPlay: true, deleteMedia: false, manageCollections: false,
        editSubtitles: false, remoteControl: true, maxSessions: 0)
}
