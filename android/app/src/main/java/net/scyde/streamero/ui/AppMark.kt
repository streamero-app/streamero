package net.scyde.streamero.ui

import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import net.scyde.streamero.R

/** The brand mark on its own (the app icon), without the wordmark. */
@Composable
internal fun AppMark(size: Dp = 56.dp, modifier: Modifier = Modifier) {
    Image(
        painter = painterResource(R.mipmap.ic_launcher),
        contentDescription = stringResource(R.string.app_name),
        modifier = modifier.size(size).clip(RoundedCornerShape(size * 0.24f)),
    )
}
