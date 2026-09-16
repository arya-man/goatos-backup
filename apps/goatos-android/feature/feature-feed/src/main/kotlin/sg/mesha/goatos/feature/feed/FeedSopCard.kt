package sg.mesha.goatos.feature.feed

import androidx.compose.foundation.lazy.LazyListScope
import sg.mesha.goatos.core.ui.sop.SopCardUi
import sg.mesha.goatos.core.ui.sop.sopCardItems

/**
 * FEED SOP (maintainer decision 2026-09-16, docs/decisions/feed-sop.md): the CARD a feed stage is
 * proven against. The model and its rendering moved to core-ui ([SopCardUi], [sopCardItems]) so the
 * herd operations capture forms render the same card; the feed names stay as aliases.
 */
typealias FeedSopCardUi = SopCardUi

fun LazyListScope.feedSopCardItems(
    card: FeedSopCardUi,
    locked: Boolean,
    onCapture: (slotKey: String, kind: String?) -> Unit,
    onPlaybackFailed: (slotKey: String) -> Unit,
    onPreviewAction: (slotKey: String, action: String) -> Unit,
    onAnswer: (questionId: String, value: String) -> Unit,
) = sopCardItems(card, locked, onCapture, onPlaybackFailed, onPreviewAction, onAnswer)
