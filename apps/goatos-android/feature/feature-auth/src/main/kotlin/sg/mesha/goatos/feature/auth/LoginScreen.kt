package sg.mesha.goatos.feature.auth

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme

/**
 * Sign-in (`v-login`) — a faithful single-screen port of the mock's `v-login`: the brand
 * lockup, the Work-email `.inp` (with the user glyph), the One-time-code `.inp` (mono,
 * letter-spaced), the gradient Sign-in `.btn`, the 6-digit note, the App-language `.langbtn`,
 * and the HR-role note — in the mock's exact order, spacing (16px gutter, 26/30/14/16/12/22/18
 * rhythm) and type.
 *
 * Pre-session screen: email/otp/language are LOCAL hoisted state (`remember`), not a backend
 * UiState (no bootstrap contract before auth). The only external contract is [onSignIn]
 * (MainActivity calls `LoginScreen(onSignIn = sessionViewModel::signIn)`). OTP verification is
 * stubbed (any 6 digits) until the Firebase/backend auth pass; Sign-in enables once the email
 * is valid and six digits are entered.
 */
@Composable
fun LoginScreen(
    onSignIn: (email: String) -> Unit,
    modifier: Modifier = Modifier,
    errorMessage: String? = null,
) {
    var email by remember { mutableStateOf("") }
    var otp by remember { mutableStateOf("") }
    var language by remember { mutableStateOf(LANGUAGES.first()) }

    LoginContent(
        email = email,
        otp = otp,
        language = language,
        errorMessage = errorMessage,
        onEmailChange = { email = it },
        onOtpChange = { next -> if (next.length <= OTP_LENGTH && next.all(Char::isDigit)) otp = next },
        onSignIn = { if (isValidEmail(email) && otp.length == OTP_LENGTH) onSignIn(email.trim()) },
        onCycleLanguage = { language = LANGUAGES[(LANGUAGES.indexOf(language) + 1) % LANGUAGES.size] },
        modifier = modifier,
    )
}

private const val OTP_LENGTH = 6
private val LANGUAGES = listOf("English", "हिन्दी", "ಕನ್ನಡ", "తెలుగు")

private fun isValidEmail(raw: String): Boolean {
    val e = raw.trim()
    val at = e.indexOf('@')
    return at > 0 && at < e.length - 1 && e.substring(at + 1).contains('.')
}

/** Stateless renderer — all state hoisted, so `@Preview` renders any filled state with fixed props. */
@Composable
private fun LoginContent(
    email: String,
    otp: String,
    language: String,
    onEmailChange: (String) -> Unit,
    onOtpChange: (String) -> Unit,
    onSignIn: () -> Unit,
    onCycleLanguage: () -> Unit,
    modifier: Modifier = Modifier,
    errorMessage: String? = null,
) {
    val canSignIn = isValidEmail(email) && otp.length == OTP_LENGTH
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(LoginTokens.PageBg)
            .verticalScroll(rememberScrollState())
            .padding(horizontal = Gutter),
    ) {
        Spacer(Modifier.height(26.dp))
        BrandLockup()

        Spacer(Modifier.height(30.dp))
        FieldLabel("Work email")
        EmailField(email = email, onEmailChange = onEmailChange)

        Spacer(Modifier.height(14.dp))
        FieldLabel("One-time code")
        OtpField(otp = otp, onOtpChange = onOtpChange, onDone = onSignIn)

        Spacer(Modifier.height(16.dp))
        PrimaryButton(text = "Sign in", enabled = canSignIn, onClick = onSignIn)

        Spacer(Modifier.height(12.dp))
        Centered("6-digit code sent to your Mesha email")

        Spacer(Modifier.height(22.dp))
        FieldLabel("App language")
        LanguageField(language = language, onClick = onCycleLanguage)

        Spacer(Modifier.height(18.dp))
        Text(
            text = "Your role comes from the HR directory — you land on the screen for " +
                "your job. The field operator executes; managers and directors track.",
            color = LoginTokens.Faint,
            fontSize = 11.5.sp,
            fontWeight = FontWeight.W500,
            lineHeight = 18.sp,
        )

        if (!errorMessage.isNullOrBlank()) {
            Spacer(Modifier.height(16.dp))
            Text(
                text = errorMessage,
                color = LoginTokens.Danger,
                fontSize = 12.5.sp,
                fontWeight = FontWeight.W600,
                textAlign = TextAlign.Center,
                lineHeight = 18.sp,
                modifier = Modifier.fillMaxWidth(),
            )
        }
        Spacer(Modifier.height(24.dp))
    }
}

/* --------------------------------------------------------------------------- */
/* Fields                                                                      */
/* --------------------------------------------------------------------------- */

/** `.fld label` — 11px / 750 / .04em / uppercase / muted, 6px below. */
@Composable
private fun FieldLabel(text: String) {
    Text(
        text = text.uppercase(),
        color = LoginTokens.Muted,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        letterSpacing = 0.44.sp,
        modifier = Modifier.padding(bottom = 6.dp),
    )
}

@Composable
private fun Centered(text: String) {
    Text(
        text = text,
        color = LoginTokens.Faint,
        fontSize = 11.5.sp,
        fontWeight = FontWeight.W600,
        textAlign = TextAlign.Center,
        modifier = Modifier.fillMaxWidth(),
    )
}

/** `.inp` shell: surf bg, hair border, r14, 13×15 padding, min-height 50, 10px gap. */
@Composable
private fun InputShell(content: @Composable androidx.compose.foundation.layout.RowScope.() -> Unit) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(LoginTokens.Surf)
            .border(1.dp, LoginTokens.Hair, RoundedCornerShape(14.dp))
            .heightIn(min = 50.dp)
            .padding(horizontal = 15.dp, vertical = 13.dp),
        content = content,
    )
}

@Composable
private fun EmailField(email: String, onEmailChange: (String) -> Unit) {
    BasicTextField(
        value = email,
        onValueChange = onEmailChange,
        singleLine = true,
        textStyle = TextStyle(color = LoginTokens.Ink, fontSize = 14.5.sp),
        cursorBrush = SolidColor(LoginTokens.Brand),
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email, imeAction = ImeAction.Next),
        modifier = Modifier.fillMaxWidth(),
        decorationBox = { inner ->
            InputShell {
                Icon(
                    imageVector = MeshaIcons.User,
                    contentDescription = null,
                    tint = LoginTokens.Muted,
                    modifier = Modifier.size(18.dp),
                )
                Box(Modifier.weight(1f)) {
                    if (email.isEmpty()) {
                        Text("you@mesha.sg", color = LoginTokens.Faint, fontSize = 14.5.sp)
                    }
                    inner()
                }
            }
        },
    )
}

/** One-time code — the mock's single mono, letter-spaced `.inp` (not a box row). */
@Composable
private fun OtpField(otp: String, onOtpChange: (String) -> Unit, onDone: () -> Unit) {
    BasicTextField(
        value = otp,
        onValueChange = onOtpChange,
        singleLine = true,
        textStyle = TextStyle(
            color = LoginTokens.Ink,
            fontSize = 16.sp,
            fontWeight = FontWeight.W700,
            fontFamily = FontFamily.Monospace,
            letterSpacing = 5.6.sp,
        ),
        cursorBrush = SolidColor(LoginTokens.Brand),
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword, imeAction = ImeAction.Done),
        keyboardActions = KeyboardActions(onDone = { onDone() }),
        modifier = Modifier.fillMaxWidth(),
        decorationBox = { inner ->
            InputShell {
                Box(Modifier.weight(1f)) {
                    if (otp.isEmpty()) {
                        Text(
                            text = "••••••",
                            color = LoginTokens.Faint,
                            fontSize = 16.sp,
                            fontFamily = FontFamily.Monospace,
                            letterSpacing = 5.6.sp,
                        )
                    }
                    inner()
                }
            }
        },
    )
}

/** `.langbtn` — globe chip + language name + "Change ›". */
@Composable
private fun LanguageField(language: String, onClick: () -> Unit) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(LoginTokens.Surf)
            .border(1.dp, LoginTokens.Hair, RoundedCornerShape(14.dp))
            .clickable(onClick = onClick)
            .heightIn(min = 50.dp)
            .padding(horizontal = 15.dp, vertical = 13.dp),
    ) {
        Box(
            modifier = Modifier
                .size(26.dp)
                .clip(RoundedCornerShape(9.dp))
                .background(LoginTokens.BrandTint),
            contentAlignment = Alignment.Center,
        ) {
            Icon(MeshaIcons.Globe, contentDescription = null, tint = LoginTokens.Brand, modifier = Modifier.size(15.dp))
        }
        Spacer(Modifier.width(12.dp))
        Text(language, color = LoginTokens.Ink, fontSize = 14.5.sp)
        Spacer(Modifier.weight(1f))
        Text("Change", color = LoginTokens.Faint, fontSize = 13.sp, fontWeight = FontWeight.W600)
        Spacer(Modifier.width(2.dp))
        Icon(MeshaIcons.Chevron, contentDescription = null, tint = LoginTokens.Faint, modifier = Modifier.size(13.dp))
    }
}

/* --------------------------------------------------------------------------- */
/* Chrome                                                                      */
/* --------------------------------------------------------------------------- */

@Composable
private fun BrandLockup() {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(
            modifier = Modifier
                .size(40.dp)
                .clip(CircleShape)
                .background(LoginTokens.BrandTint)
                .border(2.dp, LoginTokens.Brand, CircleShape),
            contentAlignment = Alignment.Center,
        ) {
            // Mesha brand mark = Devanagari "मे" (mock `.logo`), NOT a Latin "M".
            Text(text = "मे", color = LoginTokens.Brand, fontSize = 16.sp, fontWeight = FontWeight.W800)
        }
        Spacer(Modifier.width(13.dp))
        Column {
            Text(
                text = "Mesha",
                color = LoginTokens.Ink,
                fontSize = 22.sp,
                fontWeight = FontWeight.W900,
                letterSpacing = (-0.66).sp,
            )
            Text(
                text = "Field operations",
                color = LoginTokens.Faint,
                fontSize = 12.sp,
                fontWeight = FontWeight.W600,
            )
        }
    }
}

/** `.btn` — gradient, r15, min-height 52, 15px / 780. Disabled = surf + hair (mock `.btn.block`). */
@Composable
private fun PrimaryButton(text: String, enabled: Boolean, onClick: () -> Unit) {
    val shape = RoundedCornerShape(15.dp)
    val base = Modifier.fillMaxWidth().height(52.dp).clip(shape)
    val styled = if (enabled) {
        base.background(LoginTokens.BrandGradient, shape)
    } else {
        base.background(LoginTokens.Surf, shape).border(1.dp, LoginTokens.Hair, shape)
    }
    Box(
        modifier = styled.clickable(enabled = enabled, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = text,
            color = if (enabled) LoginTokens.OnBrand else LoginTokens.Faint,
            fontSize = 15.sp,
            fontWeight = FontWeight.W800,
        )
    }
}

private val Gutter = 16.dp

/* --------------------------------------------------------------------------- */
/* Tokens (mock dark palette — design-system.md §1)                            */
/* --------------------------------------------------------------------------- */

private object LoginTokens {
    val Brand = Color(0xFF8AD457)
    val BrandTint = Color(0x248AD457)
    val OnBrand = Color(0xFF08130B)
    val PageBg = Color(0xFF0A0F0C)
    val Surf = Color(0xFF131A15)
    val Hair = Color(0xFF28352B)
    val Ink = Color(0xFFECF4EE)
    val Muted = Color(0xFF8FA497)
    val Faint = Color(0xFF5F7367)
    val Danger = Color(0xFFFB6F63)
    val BrandGradient: Brush = Brush.linearGradient(listOf(Color(0xFF93DA5E), Color(0xFF5FB531)))
}

/* --------------------------------------------------------------------------- */
/* Preview                                                                     */
/* --------------------------------------------------------------------------- */

@Preview(name = "Login", showBackground = true, backgroundColor = 0xFF0A0F0C, widthDp = 380, heightDp = 820)
@Composable
private fun LoginPreview() {
    GoatOsTheme {
        LoginContent(
            email = "arun.kumar@mesha.sg",
            otp = "4290",
            language = "English",
            onEmailChange = {},
            onOtpChange = {},
            onSignIn = {},
            onCycleLanguage = {},
        )
    }
}
