package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.paging.PagingData
import androidx.paging.compose.collectAsLazyPagingItems
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import com.android.resources.Density
import kotlinx.coroutines.flow.flowOf
import org.junit.Rule
import org.junit.Test
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.feature.vendors.FeedPurchaseCardUi
import sg.mesha.goatos.feature.vendors.FeedPurchasesListScreen
import sg.mesha.goatos.feature.vendors.FeedPurchasesListUiState
import sg.mesha.goatos.feature.vendors.VendorsTone

/**
 * Feed purchase cards at phone width (Sales phone E2E 2026-09-26): a full feed name beside the
 * delivery chip was cut to "Mesha Adult Conc…". The name wraps to a second line instead, and the
 * rest of the card (load, quantity, date, payment) is still all there.
 */
class FeedPurchasesListScreenshotTest {
    @get:Rule
    // The Realme the defect was seen on: 1080 x 2400 at 480 dpi, i.e. a 360 dp wide screen.
    val paparazzi = Paparazzi(
        deviceConfig = DeviceConfig.PIXEL_5.copy(screenWidth = 1080, screenHeight = 2400, density = Density.XXHIGH, xdpi = 480, ydpi = 480),
    )

    @Test
    fun longFeedNames() {
        val cards = listOf(
            // Real catalog names from the farm's ledger, the longest first.
            card("1", "Mesha Adult Concentrate Sheep", "In transit", VendorsTone.WARN, "Part paid", VendorsTone.WARN),
            card("2", "Mesha Kids Sheep Concentrate", "Delivered", VendorsTone.OK, "Paid", VendorsTone.OK),
            card("3", "Dry Masoor Bhusa", "Delivered", VendorsTone.OK, "Not paid", VendorsTone.DANGER),
        )
        paparazzi.snapshot(name = "feed_purchases_long_feed_names") {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        FeedPurchasesListScreen(
                            state = FeedPurchasesListUiState(title = "Feed purchases", totalsLine = "3 loads · 12,500 kg · ₹4,10,000", canAdd = true),
                            rows = flowOf(PagingData.from(cards)).collectAsLazyPagingItems(),
                        )
                    }
                }
            }
        }
    }

    private fun card(id: String, feed: String, delivery: String, deliveryTone: VendorsTone, payment: String, paymentTone: VendorsTone) =
        FeedPurchaseCardUi(
            listKey = id, purchaseId = id, feedItem = feed, loadLine = "CBE · Load 32$id",
            quantityLine = "4,000 kg · ₹1,20,000", metaLine = "Bought 24/09/2026 · Sri Lakshmi Traders",
            deliveryLabel = delivery, deliveryTone = deliveryTone, paymentLabel = payment, paymentTone = paymentTone,
        )
}
