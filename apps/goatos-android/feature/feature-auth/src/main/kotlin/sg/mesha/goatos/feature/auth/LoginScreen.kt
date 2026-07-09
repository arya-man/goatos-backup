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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
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
 * Sign-in (`v-login`). Two-step work-email → OTP flow, ported from the mock's
 * `v-login` anatomy (logo lockup, `.fld`/`.inp` fields, gradient `.btn`, `.langbtn`).
 *
 * This is a PRE-SESSION screen, so its step/email/otp are LOCAL hoisted state
 * (`remember`) rather than a backend-fed UiState — there is no bootstrap contract
 * before auth (the TRD's pre-contract auth exception). The only external contract
 * is [onSignIn]; the signature is intentionally unchanged because MainActivity
 * calls `LoginScreen(onSignIn = sessionViewModel::signIn)`.
 *
 * Step 1: work email + "Send code". Step 2: 6-digit OTP + "Verify" + "Resend code"
 * + a change-email back affordance. OTP verification is stubbed for now (any six
 * digits is accepted); real Firebase/backend OTP lands with the auth pass.
 */
@Composable
fun LoginScreen(
    onSignIn: (email: String) -> Unit,
    modifier: Modifier = Modifier,
    errorMessage: String? = null,
) {
    var step by remember { mutableStateOf(LoginStep.Email) }
    var email by remember { mutableStateOf("") }
    var otp by remember { mutableStateOf("") }

    LoginContent(
        step = step,
        email = email,
        otp = otp,
        errorMessage = errorMessage,
        onEmailChange = { email = it },
        onOtpChange = { otp = it },
        onSendCode = {
            if (isValidEmail(email)) {
                otp = ""
                step = LoginStep.Otp
            }
        },
        onVerify = {
            // TODO: real OTP verify via backend/Firebase. For now any 6-digit code
            // is accepted and we hand the email to the caller to start the session.
            if (otp.length == OTP_LENGTH) onSignIn(email.trim())
        },
        onChangeEmail = {
            otp = ""
            step = LoginStep.Email
        },
        onResend = {
            // TODO: real resend (re-request OTP) via backend/Firebase.
            otp = ""
        },
        modifier = modifier,
    )
}

private enum class LoginStep { Email, Otp }

private const val OTP_LENGTH = 6

private fun isValidEmail(raw: String): Boolean {
    val e = raw.trim()
    val at = e.indexOf('@')
    return at > 0 && at < e.length - 1 && e.substring(at + 1).contains('.')
}

/**
 * Stateless renderer for both steps — all state is hoisted to [LoginScreen], so the
 * `@Preview` can render any step (here: the OTP step) with fixed props.
 */
@Composable
private fun LoginContent(
    step: LoginStep,
    email: String,
    otp: String,
    onEmailChange: (String) -> Unit,
    onOtpChange: (String) -> Unit,
    onSendCode: () -> Unit,
    onVerify: () -> Unit,
    onChangeEmail: () -> Unit,
    onResend: () -> Unit,
    modifier: Modifier = Modifier,
    errorMessage: String? = null,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(LoginTokens.PageBg)
            .padding(horizontal = 20.dp),
    ) {
        Row(
            modifier = Modifier.fillMaxWidth().padding(top = 14.dp, bottom = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            if (step == LoginStep.Otp) {
                ChangeEmailAffordance(onClick = onChangeEmail)
            } else {
                Spacer(Modifier.width(1.dp))
            }
            LanguageChip()
        }

        Spacer(Modifier.height(26.dp))
        BrandLockup()
        Spacer(Modifier.height(30.dp))

        when (step) {
            LoginStep.Email -> EmailStep(
                email = email,
                onEmailChange = onEmailChange,
                onSendCode = onSendCode,
            )
            LoginStep.Otp -> OtpStep(
                email = email,
                otp = otp,
                onOtpChange = onOtpChange,
                onVerify = onVerify,
                onResend = onResend,
            )
        }

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
    }
}

/* --------------------------------------------------------------------------- */
/* Steps                                                                       */
/* --------------------------------------------------------------------------- */

@Composable
private fun EmailStep(
    email: String,
    onEmailChange: (String) -> Unit,
    onSendCode: () -> Unit,
) {
    val valid = isValidEmail(email)
    FieldLabel("Work email")
    EmailField(
        email = email,
        onEmailChange = onEmailChange,
        onSubmit = { if (valid) onSendCode() },
    )
    Spacer(Modifier.height(16.dp))
    PrimaryButton(text = "Send code", enabled = valid, onClick = onSendCode)
    Spacer(Modifier.height(12.dp))
    Text(
        text = "We'll email you a 6-digit code.",
        color = LoginTokens.Faint,
        fontSize = 11.5.sp,
        fontWeight = FontWeight.W600,
        textAlign = TextAlign.Center,
        modifier = Modifier.fillMaxWidth(),
    )
    Spacer(Modifier.height(22.dp))
    Text(
        text = "Your role comes from the HR directory — you land on the screen for " +
            "your job. The field operator executes; managers and directors track.",
        color = LoginTokens.Faint,
        fontSize = 11.5.sp,
        fontWeight = FontWeight.W500,
        lineHeight = 17.sp,
    )
}

@Composable
private fun OtpStep(
    email: String,
    otp: String,
    onOtpChange: (String) -> Unit,
    onVerify: () -> Unit,
    onResend: () -> Unit,
) {
    val complete = otp.length == OTP_LENGTH
    Text(
        text = "Enter the 6-digit code sent to",
        color = LoginTokens.Muted,
        fontSize = 13.5.sp,
        fontWeight = FontWeight.W600,
    )
    Text(
        text = email.ifBlank { "your Mesha email" },
        color = LoginTokens.Ink,
        fontSize = 14.sp,
        fontWeight = FontWeight.W700,
        fontFamily = FontFamily.Monospace,
        modifier = Modifier.padding(top = 2.dp),
    )
    Spacer(Modifier.height(20.dp))
    FieldLabel("One-time code")
    OtpField(otp = otp, onOtpChange = onOtpChange, onComplete = { if (complete) onVerify() })
    Spacer(Modifier.height(16.dp))
    PrimaryButton(text = "Verify", enabled = complete, onClick = onVerify)
    Spacer(Modifier.height(14.dp))
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.Center,
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = "Didn't get the code?",
            color = LoginTokens.Faint,
            fontSize = 12.sp,
            fontWeight = FontWeight.W600,
        )
        Spacer(Modifier.width(6.dp))
        Text(
            text = "Resend code",
            color = LoginTokens.Brand,
            fontSize = 12.sp,
            fontWeight = FontWeight.W700,
            modifier = Modifier
                .clip(RoundedCornerShape(8.dp))
                .clickable(onClick = onResend)
                .padding(horizontal = 6.dp, vertical = 4.dp),
        )
    }
}

/* --------------------------------------------------------------------------- */
/* Fields                                                                      */
/* --------------------------------------------------------------------------- */

@Composable
private fun FieldLabel(text: String) {
    Text(
        text = text.uppercase(),
        color = LoginTokens.Muted,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        letterSpacing = 0.5.sp,
        modifier = Modifier.padding(bottom = 6.dp),
    )
}

@Composable
private fun EmailField(
    email: String,
    onEmailChange: (String) -> Unit,
    onSubmit: () -> Unit,
) {
    BasicTextField(
        value = email,
        onValueChange = onEmailChange,
        singleLine = true,
        textStyle = TextStyle(color = LoginTokens.Ink, fontSize = 15.sp),
        cursorBrush = SolidColor(LoginTokens.Brand),
        keyboardOptions = KeyboardOptions(
            keyboardType = KeyboardType.Email,
            imeAction = ImeAction.Done,
        ),
        keyboardActions = KeyboardActions(onDone = { onSubmit() }),
        modifier = Modifier.fillMaxWidth(),
        decorationBox = { inner ->
            Row(
                verticalAlignment = Alignment.CenterVertically,
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(14.dp))
                    .background(LoginTokens.Surf)
                    .border(1.dp, LoginTokens.Hair, RoundedCornerShape(14.dp))
                    .heightIn(min = 52.dp)
                    .padding(horizontal = 15.dp, vertical = 13.dp),
            ) {
                Box(Modifier.weight(1f)) {
                    if (email.isEmpty()) {
                        Text(
                            text = "you@mesha.sg",
                            color = LoginTokens.Faint,
                            fontSize = 15.sp,
                        )
                    }
                    inner()
                }
            }
        },
    )
}

@Composable
private fun OtpField(
    otp: String,
    onOtpChange: (String) -> Unit,
    onComplete: () -> Unit,
) {
    val focusRequester = remember { FocusRequester() }
    LaunchedEffect(Unit) { runCatching { focusRequester.requestFocus() } }

    BasicTextField(
        value = otp,
        onValueChange = { next ->
            if (next.length <= OTP_LENGTH && next.all { it.isDigit() }) {
                onOtpChange(next)
                if (next.length == OTP_LENGTH) onComplete()
            }
        },
        singleLine = true,
        // Digits render inside the boxes below; the field text itself is invisible.
        textStyle = TextStyle(color = Color.Transparent),
        cursorBrush = SolidColor(Color.Transparent),
        keyboardOptions = KeyboardOptions(
            keyboardType = KeyboardType.NumberPassword,
            imeAction = ImeAction.Done,
        ),
        keyboardActions = KeyboardActions(onDone = { onComplete() }),
        modifier = Modifier.fillMaxWidth().focusRequester(focusRequester),
        decorationBox = {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                repeat(OTP_LENGTH) { index ->
                    val char = otp.getOrNull(index)
                    val active = index == otp.length
                    Box(
                        modifier = Modifier
                            .weight(1f)
                            .heightIn(min = 54.dp)
                            .clip(RoundedCornerShape(12.dp))
                            .background(LoginTokens.Surf)
                            .border(
                                width = if (active) 1.5.dp else 1.dp,
                                color = if (active) LoginTokens.Brand else LoginTokens.Hair,
                                shape = RoundedCornerShape(12.dp),
                            ),
                        contentAlignment = Alignment.Center,
                    ) {
                        Text(
                            text = char?.toString() ?: "",
                            color = LoginTokens.Ink,
                            fontSize = 20.sp,
                            fontWeight = FontWeight.W800,
                            fontFamily = FontFamily.Monospace,
                        )
                    }
                }
            }
        },
    )
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
            Text(
                text = "मे",
                color = LoginTokens.Brand,
                fontSize = 16.sp,
                fontWeight = FontWeight.W800,
            )
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

@Composable
private fun LanguageChip() {
    // No-op affordance for now — opens the language sheet with the real i18n pass.
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(LoginTokens.Surf)
            .border(1.dp, LoginTokens.Hair, RoundedCornerShape(999.dp))
            .clickable { /* TODO: open language sheet (ovl-lang) */ }
            .padding(horizontal = 12.dp, vertical = 7.dp),
    ) {
        Text("EN", color = LoginTokens.Ink, fontSize = 12.sp, fontWeight = FontWeight.W700)
        Spacer(Modifier.width(4.dp))
        Text("▾", color = LoginTokens.Faint, fontSize = 11.sp, fontWeight = FontWeight.W700)
    }
}

@Composable
private fun ChangeEmailAffordance(onClick: () -> Unit) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 8.dp, vertical = 6.dp),
    ) {
        Icon(imageVector = MeshaIcons.ChevronLeft, contentDescription = null, tint = LoginTokens.Muted, modifier = Modifier.size(18.dp))
        Spacer(Modifier.width(4.dp))
        Text(
            text = "Change email",
            color = LoginTokens.Muted,
            fontSize = 12.5.sp,
            fontWeight = FontWeight.W600,
        )
    }
}

@Composable
private fun PrimaryButton(
    text: String,
    enabled: Boolean,
    onClick: () -> Unit,
) {
    val shape = RoundedCornerShape(15.dp)
    val base = Modifier
        .fillMaxWidth()
        .height(52.dp)
        .clip(shape)
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
            fontWeight = FontWeight.W700,
        )
    }
}

/* --------------------------------------------------------------------------- */
/* Tokens (ported from the mock's dark palette — design-system.md §1)          */
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
    val BrandGradient: Brush = Brush.linearGradient(
        listOf(Color(0xFF93DA5E), Color(0xFF5FB531)),
    )
}

/* --------------------------------------------------------------------------- */
/* Preview                                                                     */
/* --------------------------------------------------------------------------- */

@Preview(
    name = "Login · OTP step",
    showBackground = true,
    backgroundColor = 0xFF0A0F0C,
    widthDp = 380,
    heightDp = 780,
)
@Composable
private fun LoginOtpStepPreview() {
    GoatOsTheme {
        LoginContent(
            step = LoginStep.Otp,
            email = "arun.kumar@mesha.sg",
            otp = "4290",
            onEmailChange = {},
            onOtpChange = {},
            onSendCode = {},
            onVerify = {},
            onChangeEmail = {},
            onResend = {},
        )
    }
}

@Preview(
    name = "Login · Email step",
    showBackground = true,
    backgroundColor = 0xFF0A0F0C,
    widthDp = 380,
    heightDp = 780,
)
@Composable
private fun LoginEmailStepPreview() {
    GoatOsTheme {
        LoginContent(
            step = LoginStep.Email,
            email = "",
            otp = "",
            onEmailChange = {},
            onOtpChange = {},
            onSendCode = {},
            onVerify = {},
            onChangeEmail = {},
            onResend = {},
        )
    }
}
