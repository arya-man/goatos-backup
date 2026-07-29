package sg.mesha.goatos.core.designsystem.theme

import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp

/**
 * The single source of type truth — the mock's type scale (size / weight / tracking).
 * Colour is intentionally NOT baked in: screens pass a [MeshaColors] value at the use site,
 * so type and colour stay orthogonal. Screens MUST use these instead of ad-hoc fontSize/weight.
 */
object MeshaType {
    /** `.vhead .eb` — small uppercase overline above a header title. */
    val overline = TextStyle(fontSize = 10.5.sp, fontWeight = FontWeight.W700, letterSpacing = 0.42.sp)

    /** `.eyebrow` — the hero eyebrow (brand-2, wider tracking). */
    val eyebrow = TextStyle(fontSize = 11.5.sp, fontWeight = FontWeight.W700, letterSpacing = 1.6.sp)

    /** Big screen title ("Calendar", "You", "Alerts"). */
    val screenTitle = TextStyle(fontSize = 22.sp, fontWeight = FontWeight.W700)

    /** `.vhead h3` — compact in-header title. */
    val headerTitle = TextStyle(fontSize = 16.5.sp, fontWeight = FontWeight.W700, letterSpacing = (-0.3).sp)

    /** `.dl` — uppercase section label (apply .uppercase() at the call site). */
    val sectionLabel = TextStyle(fontSize = 11.sp, fontWeight = FontWeight.W700, letterSpacing = 0.55.sp)

    /** `.fld label` — uppercase field label. */
    val fieldLabel = TextStyle(fontSize = 11.sp, fontWeight = FontWeight.W700, letterSpacing = 0.44.sp)

    /** `.evt .nm` / `.card` title. */
    val cardTitle = TextStyle(fontSize = 15.sp, fontWeight = FontWeight.W700, letterSpacing = (-0.15).sp)

    /** `.evt .mt` / secondary card text. */
    val cardSubtitle = TextStyle(fontSize = 12.sp, fontWeight = FontWeight.W500)

    /** Dense evidence/feed row title, matching alert/list cards without using full card-title scale. */
    val listTitle = TextStyle(fontSize = 13.5.sp, fontWeight = FontWeight.W700)

    /** Default body / input text. */
    val body = TextStyle(fontSize = 14.5.sp, fontWeight = FontWeight.W400)
    val bodyStrong = TextStyle(fontSize = 14.5.sp, fontWeight = FontWeight.W700)

    /** `.pill`. */
    val pill = TextStyle(fontSize = 11.sp, fontWeight = FontWeight.W700)

    /** `.btn`. */
    val button = TextStyle(fontSize = 15.sp, fontWeight = FontWeight.W800)

    /** `.clk` — the green "Open drive ›" affordance. */
    val cta = TextStyle(fontSize = 12.5.sp, fontWeight = FontWeight.W700)

    /** `.week .d .dn` (day letter) + `.dd` (day number). */
    val dayName = TextStyle(fontSize = 9.5.sp, fontWeight = FontWeight.W700)
    val dayNumber = TextStyle(fontSize = 15.sp, fontWeight = FontWeight.W800)

    /** Fine print (otp note, role note). */
    val caption = TextStyle(fontSize = 11.5.sp, fontWeight = FontWeight.W600)
}
