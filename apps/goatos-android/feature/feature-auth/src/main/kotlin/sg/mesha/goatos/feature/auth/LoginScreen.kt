package sg.mesha.goatos.feature.auth

// telemetry: analytics tracked in boot/SessionViewModel.kt (LOGIN_ATTEMPT, LOGIN_SUCCESS, LOGIN_FAILURE)
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaInputShell
import sg.mesha.goatos.core.designsystem.component.MeshaLanguageSheet
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.locale.AppLocaleState
import sg.mesha.goatos.core.designsystem.locale.LocalAppLanguage
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import kotlinx.coroutines.delay

/**
 * Sign-in (`v-login`) with Google SSO, email/password, and password reset. The
 * feature module owns only presentation; real credential verification lives in
 * SessionViewModel in :app.
 *
 * telemetry:exempt Login attempt, success, failure, and password-reset events are owned by
 * SessionViewModel so this presentation-only composable cannot double-count them.
 */
@Composable
fun LoginScreen(
    onSignInEmail: (email: String, password: String) -> Unit,
    onGoogle: () -> Unit,
    onForgotPassword: (email: String) -> Unit,
    modifier: Modifier = Modifier,
    isLoading: Boolean = false,
    errorReason: LoginError? = null,
    errorDetail: String? = null,
    resetEmailSent: String? = null,
    // telemetry: forwarded to PermissionGateCard, which owns no analytics client itself (no
    // Hilt in this module) — MainActivity wires these to the injected AnalyticsPort.
    onPermissionGateShown: (missingPermissions: List<String>) -> Unit = {},
    onPermissionAnswered: (permission: String, granted: Boolean) -> Unit = { _, _ -> },
) {
    var email by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }

    LoginContent(
        email = email,
        password = password,
        onEmailChange = { email = it },
        onPasswordChange = { password = it },
        onSignIn = { if (isValidEmail(email) && password.isNotBlank() && !isLoading) onSignInEmail(email.trim(), password) },
        onGoogle = { if (!isLoading) onGoogle() },
        onForgotPassword = { if (email.isNotBlank() && !isLoading) onForgotPassword(email.trim()) },
        isLoading = isLoading,
        errorText = resolveErrorText(errorReason, errorDetail),
        resetEmailSent = resetEmailSent,
        onPermissionGateShown = onPermissionGateShown,
        onPermissionAnswered = onPermissionAnswered,
        modifier = modifier,
    )
}

private fun isValidEmail(raw: String): Boolean {
    val e = raw.trim()
    val at = e.indexOf('@')
    return at > 0 && at < e.length - 1 && e.substring(at + 1).contains('.')
}

@Composable
private fun resolveErrorText(reason: LoginError?, detail: String?): String? = when (reason) {
    null -> null
    LoginError.INVALID_CREDENTIALS -> stringResource(R.string.login_error_invalid_credentials)
    LoginError.NETWORK -> stringResource(R.string.login_error_network)
    LoginError.TOO_MANY_REQUESTS -> stringResource(R.string.login_error_too_many_requests)
    LoginError.GOOGLE_CANCELLED -> stringResource(R.string.login_error_google_cancelled)
    LoginError.NO_GOOGLE_ACCOUNT -> stringResource(R.string.login_error_no_google_account)
    LoginError.NO_DEV_BACKEND -> stringResource(R.string.login_error_no_dev_backend)
    LoginError.UNKNOWN -> detail?.takeIf { it.isNotBlank() } ?: stringResource(R.string.login_error_generic)
}

@Composable
private fun LoginContent(
    email: String,
    password: String,
    onEmailChange: (String) -> Unit,
    onPasswordChange: (String) -> Unit,
    onSignIn: () -> Unit,
    onGoogle: () -> Unit,
    onForgotPassword: () -> Unit,
    modifier: Modifier = Modifier,
    isLoading: Boolean = false,
    errorText: String? = null,
    resetEmailSent: String? = null,
    onPermissionGateShown: (missingPermissions: List<String>) -> Unit = {},
    onPermissionAnswered: (permission: String, granted: Boolean) -> Unit = { _, _ -> },
) {
    val canSignIn = isValidEmail(email) && password.isNotBlank() && !isLoading
    val currentTag = LocalAppLanguage.current
    var showLangSheet by remember { mutableStateOf(false) }
    val scrollState = rememberScrollState()
    var passwordFocused by remember { mutableStateOf(false) }
    LaunchedEffect(passwordFocused) {
        if (passwordFocused) {
            delay(250)
            scrollState.animateScrollTo(scrollState.maxValue)
        }
    }
    if (showLangSheet) {
        MeshaLanguageSheet(
            currentTag = currentTag,
            onSelect = { AppLocaleState.set(it); showLangSheet = false },
            onDismiss = { showLangSheet = false },
        )
    }
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.PageBg)
            .verticalScroll(scrollState)
            .windowInsetsPadding(WindowInsets.safeDrawing)
            .imePadding()
            .padding(horizontal = MeshaDimens.gutter),
    ) {
        Spacer(Modifier.height(44.dp))
        CenteredBrand()

        if (!errorText.isNullOrBlank()) {
            Spacer(Modifier.height(MeshaDimens.space5))
            StatusBanner(text = errorText, fg = MeshaColors.Danger, bg = MeshaColors.DangerX, icon = MeshaIcons.Warn)
        }
        if (!resetEmailSent.isNullOrBlank()) {
            Spacer(Modifier.height(MeshaDimens.space5))
            StatusBanner(
                text = stringResource(R.string.login_reset_sent, resetEmailSent),
                fg = MeshaColors.Ok,
                bg = MeshaColors.OkX,
                icon = MeshaIcons.Check,
            )
        }

        Spacer(Modifier.height(MeshaDimens.space6))
        AuthCard {
            GoogleSignInButton(enabled = !isLoading, onClick = onGoogle)

            Spacer(Modifier.height(MeshaDimens.space5))
            OrDivider()

            Spacer(Modifier.height(MeshaDimens.space5))
            FieldLabel(stringResource(R.string.login_field_work_email))
            EmailField(email = email, onEmailChange = onEmailChange, enabled = !isLoading)

            Spacer(Modifier.height(MeshaDimens.space4))
            FieldLabel(stringResource(R.string.login_field_password))
            PasswordField(
                password = password,
                onPasswordChange = onPasswordChange,
                onDone = onSignIn,
                enabled = !isLoading,
                onFocusChange = { passwordFocused = it },
            )

            Spacer(Modifier.height(MeshaDimens.space2))
            Row(modifier = Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                ForgotPasswordLink(enabled = email.isNotBlank() && !isLoading, onClick = onForgotPassword)
            }

            Spacer(Modifier.height(MeshaDimens.space5))
            LoginPrimaryButton(
                text = stringResource(if (isLoading) R.string.login_signing_in else R.string.login_sign_in),
                enabled = canSignIn,
                onClick = onSignIn,
            )

            Spacer(Modifier.height(MeshaDimens.space3))
            Centered(stringResource(R.string.login_use_email_password))
        }

        // Optional login-time device-permission readiness card (camera/BLE/notifications) —
        // renders nothing once every OS-required permission is already granted.
        Spacer(Modifier.height(MeshaDimens.space6))
        PermissionGateCard(
            onGateShown = onPermissionGateShown,
            onPermissionAnswered = onPermissionAnswered,
        )

        Spacer(Modifier.height(MeshaDimens.space6))
        FieldLabel(stringResource(R.string.login_app_language))
        LanguageField(language = AppLocaleState.labelFor(currentTag), onClick = { showLangSheet = true })

        Spacer(Modifier.height(MeshaDimens.space8))
    }
}

@Composable
private fun AuthCard(content: @Composable ColumnScope.() -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.Surf2)
            .border(MeshaDimens.hairline, MeshaColors.Hair, RoundedCornerShape(MeshaDimens.radiusCard))
            .padding(horizontal = MeshaDimens.gutter, vertical = 18.dp),
        content = content,
    )
}

@Composable
private fun LoginPrimaryButton(text: String, enabled: Boolean, onClick: () -> Unit) {
    val shape = RoundedCornerShape(MeshaDimens.radiusButton)
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = MeshaDimens.minTapButton)
            .clip(shape)
            .alpha(if (enabled) 1f else 0.5f)
            .background(MeshaColors.BrandGradient)
            .clickable(enabled = enabled, onClick = onClick)
            .padding(15.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(text = text, color = MeshaColors.OnBrand, style = MeshaType.button)
    }
}

@Composable
private fun FieldLabel(text: String) {
    Text(
        text = text.uppercase(),
        color = MeshaColors.Muted,
        style = MeshaType.fieldLabel,
        modifier = Modifier.padding(bottom = 6.dp),
    )
}

@Composable
private fun Centered(text: String) {
    Text(
        text = text,
        color = MeshaColors.Faint,
        style = MeshaType.caption,
        textAlign = TextAlign.Center,
        modifier = Modifier.fillMaxWidth(),
    )
}

@Composable
private fun StatusBanner(text: String, fg: Color, bg: Color, icon: ImageVector) {
    val shape = RoundedCornerShape(MeshaDimens.radiusInput)
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(shape)
            .background(bg)
            .border(MeshaDimens.hairline, fg.copy(alpha = 0.35f), shape)
            .padding(horizontal = 13.dp, vertical = 11.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(icon, contentDescription = null, tint = fg, modifier = Modifier.size(MeshaDimens.iconMd))
        Spacer(Modifier.width(10.dp))
        Text(
            text = text,
            color = fg,
            style = MeshaType.cardSubtitle.copy(fontWeight = FontWeight.W600),
            lineHeight = 18.sp,
        )
    }
}

@Composable
private fun EmailField(email: String, onEmailChange: (String) -> Unit, enabled: Boolean = true) {
    BasicTextField(
        value = email,
        onValueChange = onEmailChange,
        enabled = enabled,
        singleLine = true,
        textStyle = MeshaType.body.copy(color = MeshaColors.Ink),
        cursorBrush = SolidColor(MeshaColors.Brand),
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email, imeAction = ImeAction.Next),
        modifier = Modifier.fillMaxWidth(),
        decorationBox = { inner ->
            MeshaInputShell {
                Icon(MeshaIcons.User, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(MeshaDimens.iconMd))
                Box(Modifier.weight(1f)) {
                    if (email.isEmpty()) Text(stringResource(R.string.login_email_hint), color = MeshaColors.Faint, style = MeshaType.body)
                    inner()
                }
            }
        },
    )
}

@Composable
private fun PasswordField(
    password: String,
    onPasswordChange: (String) -> Unit,
    onDone: () -> Unit,
    enabled: Boolean = true,
    onFocusChange: (Boolean) -> Unit = {},
) {
    var visible by remember { mutableStateOf(false) }
    BasicTextField(
        value = password,
        onValueChange = onPasswordChange,
        enabled = enabled,
        singleLine = true,
        visualTransformation = if (visible) VisualTransformation.None else PasswordVisualTransformation(),
        textStyle = MeshaType.body.copy(color = MeshaColors.Ink),
        cursorBrush = SolidColor(MeshaColors.Brand),
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Done),
        keyboardActions = KeyboardActions(onDone = { onDone() }),
        modifier = Modifier
            .fillMaxWidth()
            .onFocusChanged { onFocusChange(it.isFocused) },
        decorationBox = { inner ->
            MeshaInputShell {
                Box(Modifier.weight(1f)) {
                    if (password.isEmpty()) Text(stringResource(R.string.login_password_hint), color = MeshaColors.Faint, style = MeshaType.body)
                    inner()
                }
                Icon(
                    imageVector = if (visible) MeshaIcons.EyeOff else MeshaIcons.Eye,
                    contentDescription = stringResource(if (visible) R.string.login_hide_password else R.string.login_show_password),
                    tint = MeshaColors.Muted,
                    modifier = Modifier
                        .size(MeshaDimens.iconMd)
                        .clickable(enabled = enabled) { visible = !visible },
                )
            }
        },
    )
}

@Composable
private fun ForgotPasswordLink(enabled: Boolean, onClick: () -> Unit) {
    Text(
        text = stringResource(R.string.login_forgot_password),
        color = if (enabled) MeshaColors.Brand else MeshaColors.Faint,
        style = MeshaType.cardSubtitle.copy(fontWeight = FontWeight.W700),
        modifier = Modifier
            .minimumInteractiveComponentSize()
            .clickable(enabled = enabled, onClick = onClick),
    )
}

@Composable
private fun LanguageField(language: String, onClick: () -> Unit) {
    MeshaInputShell(onClick = onClick) {
        Box(
            modifier = Modifier.size(MeshaDimens.glyphTile).clip(RoundedCornerShape(9.dp)).background(MeshaColors.BrandTint),
            contentAlignment = Alignment.Center,
        ) {
            Icon(MeshaIcons.Globe, contentDescription = null, tint = MeshaColors.Brand, modifier = Modifier.size(MeshaDimens.iconSm))
        }
        Text(language, color = MeshaColors.Ink, style = MeshaType.body)
        Spacer(Modifier.weight(1f))
        Text(stringResource(R.string.login_change), color = MeshaColors.Faint, style = MeshaType.cardSubtitle.copy(fontSize = 13.sp))
        Icon(MeshaIcons.Chevron, contentDescription = null, tint = MeshaColors.Faint, modifier = Modifier.size(13.dp))
    }
}

@Composable
private fun GoogleSignInButton(enabled: Boolean, onClick: () -> Unit) {
    val shape = RoundedCornerShape(MeshaDimens.radiusButton)
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .heightIn(min = MeshaDimens.minTapButton)
            .clip(shape)
            .background(MeshaColors.Surf)
            .border(MeshaDimens.hairline, MeshaColors.Hair, shape)
            .clickable(enabled = enabled, onClick = onClick)
            .padding(horizontal = 15.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.Center,
    ) {
        GoogleGlyphTile()
        Spacer(Modifier.width(12.dp))
        Text(
            text = stringResource(R.string.login_continue_with_google),
            color = if (enabled) MeshaColors.Ink else MeshaColors.Faint,
            style = MeshaType.button,
        )
    }
}

@Composable
private fun GoogleGlyphTile() {
    Box(
        modifier = Modifier.size(22.dp).clip(RoundedCornerShape(6.dp)).background(Color.White),
        contentAlignment = Alignment.Center,
    ) {
        Text(text = "G", color = MeshaColors.GoogleBrandBlue, fontSize = 14.sp, fontWeight = FontWeight.W900)
    }
}

@Composable
private fun OrDivider() {
    Row(verticalAlignment = Alignment.CenterVertically, modifier = Modifier.fillMaxWidth()) {
        Box(Modifier.weight(1f).height(MeshaDimens.hairline).background(MeshaColors.Hair))
        Text(
            text = stringResource(R.string.login_or_divider).uppercase(),
            color = MeshaColors.Faint,
            style = MeshaType.caption,
            modifier = Modifier.padding(horizontal = MeshaDimens.space3),
        )
        Box(Modifier.weight(1f).height(MeshaDimens.hairline).background(MeshaColors.Hair))
    }
}

@Composable
private fun CenteredBrand() {
    Column(
        modifier = Modifier.fillMaxWidth(),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        // Brand mark = the exact web `.logo` PNG (green ring + tint fill + centered "मे"),
        // shared with the launcher/splash icon. Image (not Compose Text) so the glyph is
        // pixel-identical to web and centered by construction — no baseline drift.
        Image(
            painter = painterResource(R.drawable.mesha_logo),
            contentDescription = "Mesha",
            modifier = Modifier.size(60.dp),
        )
        Spacer(Modifier.height(MeshaDimens.space4))
        Text(
            text = stringResource(R.string.login_brand_name),
            color = MeshaColors.Ink,
            style = MeshaType.screenTitle.copy(fontWeight = FontWeight.W900, letterSpacing = (-0.5).sp),
        )
        Spacer(Modifier.height(MeshaDimens.space1))
        Text(
            text = stringResource(R.string.login_tagline),
            color = MeshaColors.Faint,
            style = MeshaType.cardSubtitle.copy(fontSize = 12.5.sp, fontWeight = FontWeight.W600),
        )
    }
}

@Preview(name = "Login", showBackground = true, backgroundColor = 0xFF0A0F0C, widthDp = 380, heightDp = 900)
@Composable
private fun LoginPreview() {
    GoatOsTheme {
        LoginContent(
            email = "arun.kumar@mesha.sg",
            password = "hunter2",
            onEmailChange = {},
            onPasswordChange = {},
            onSignIn = {},
            onGoogle = {},
            onForgotPassword = {},
        )
    }
}

@Preview(name = "Login - error", showBackground = true, backgroundColor = 0xFF0A0F0C, widthDp = 380, heightDp = 940)
@Composable
private fun LoginErrorPreview() {
    GoatOsTheme {
        LoginContent(
            email = "arun.kumar@mesha.sg",
            password = "hunter2",
            onEmailChange = {},
            onPasswordChange = {},
            onSignIn = {},
            onGoogle = {},
            onForgotPassword = {},
            errorText = "Email or password is incorrect.",
        )
    }
}
