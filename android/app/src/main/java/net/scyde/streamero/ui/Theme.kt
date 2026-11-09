package net.scyde.streamero.ui

import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp

// "Kino platno" (D-019): warm near-black, paper-white primary action, the film's own
// colour as the only accent. Same tokens as the web client and the iOS app.
object Kino {
    val bg = Color(0xFF0A0807)
    val surface = Color(0xFF14110F)
    val surface2 = Color(0xFF1C1815)
    val surface3 = Color(0xFF2A2521)
    val text = Color(0xFFF4EFE9)
    val muted = text.copy(alpha = 0.64f)
    val faint = text.copy(alpha = 0.42f)
    val border = text.copy(alpha = 0.10f)
    val accent = Color(0xFFD6763F)
    val danger = Color(0xFFFF7A66)
    val success = Color(0xFF7FD6A0)
}

/** The film colour currently lighting the interface (from the server palette). */
val LocalAccent = staticCompositionLocalOf { Kino.accent }

fun hexColor(hex: String?, fallback: Color = Kino.accent): Color =
    runCatching { Color(android.graphics.Color.parseColor(hex)) }.getOrDefault(fallback)

/** Wide, heavy caps: the title-card voice. */
val Display = TextStyle(fontWeight = FontWeight.Black, letterSpacing = (-0.02).em, lineHeight = 0.95.em)
val Head = TextStyle(fontWeight = FontWeight.ExtraBold, fontSize = 19.sp, letterSpacing = 0.01.em)
val Label = TextStyle(fontWeight = FontWeight.SemiBold, fontSize = 12.sp, letterSpacing = 0.08.em, textAlign = TextAlign.Start)

@Composable
fun StreameroTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = darkColorScheme(
            primary = Kino.text, onPrimary = Kino.bg, secondary = Kino.accent, background = Kino.bg, onBackground = Kino.text,
            surface = Kino.surface, onSurface = Kino.text, surfaceVariant = Kino.surface2, onSurfaceVariant = Kino.muted,
            outline = Kino.border, error = Kino.danger, surfaceContainer = Kino.surface, surfaceContainerHigh = Kino.surface2,
        ),
        content = content,
    )
}
