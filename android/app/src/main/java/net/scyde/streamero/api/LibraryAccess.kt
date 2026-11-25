package net.scyde.streamero.api

/**
 * Which libraries a non-admin account sees (D-054): every library, or a chosen list.
 * Adult libraries are never part of the list; they have their own grant (D-053).
 */
data class LibraryAccess(val all: Boolean = true, val ids: Set<String> = emptySet(), val newLibraries: Boolean = false) {
    fun toggle(id: String, on: Boolean) = copy(ids = if (on) ids + id else ids - id)

    /** PATCH /users/{id} body; [current] keeps the user's other permissions as they are. */
    fun toUpdate(current: UserPermissions, libraries: List<Library>): UpdateUser =
        if (all) UpdateUser(permissions = current.copy(allLibraries = true))
        else UpdateUser(permissions = current.copy(allLibraries = false), libraryIds = chosen(libraries), newLibraries = newLibraries)

    /** POST /invites body; an all-libraries invite leaves permissions to the server default. */
    fun toInvite(expiresInHours: Int, maxUses: Int, libraries: List<Library>): CreateInvite =
        if (all) CreateInvite(expiresInHours, maxUses)
        else CreateInvite(expiresInHours, maxUses, permissions = UserPermissions.NEW_ACCOUNT.copy(allLibraries = false),
            libraryIds = chosen(libraries), newLibraries = newLibraries)

    /** Chosen ids that still exist and are pickable, in a stable order. */
    private fun chosen(libraries: List<Library>): List<String> = pickableLibraries(libraries).map { it.id }.filter { it in ids }

    companion object {
        fun of(u: User) = LibraryAccess(u.permissions.allLibraries, u.libraryIds.toSet(), u.newLibraries)
    }
}

/** Libraries an admin can put on a chosen list: everything except adult ones. */
fun pickableLibraries(libraries: List<Library>): List<Library> = libraries.filter { it.kind != "adult" }
