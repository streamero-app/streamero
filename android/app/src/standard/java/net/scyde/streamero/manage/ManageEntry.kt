package net.scyde.streamero.manage

import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavHostController
import net.scyde.streamero.AppState
import net.scyde.streamero.ui.Tab

/**
 * The standard Streamero app has no Manage section: these hooks do nothing. The
 * "Streamero Manage" flavor has the real ones in src/manage/ (D-047).
 */
internal object ManageEntry {
    const val ENABLED = false

    fun tab(@Suppress("UNUSED_PARAMETER") state: AppState): Tab? = null

    fun routes(@Suppress("UNUSED_PARAMETER") graph: NavGraphBuilder, @Suppress("UNUSED_PARAMETER") state: AppState, @Suppress("UNUSED_PARAMETER") nav: NavHostController) {}
}
