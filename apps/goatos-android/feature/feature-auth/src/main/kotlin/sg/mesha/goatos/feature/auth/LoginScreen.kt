package sg.mesha.goatos.feature.auth

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
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
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaInputShell
import sg.mesha.goatos.core.designsystem.component.MeshaLanguageSheet
import sg.mesha.goatos.core.designsystem.component.MeshaPrimaryButton
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.locale.AppLocaleState
import sg.mesha.goatos.core.designsystem.locale.LocalAppLanguage
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * Sign-in (`v-login`) — work-email + password. Everything visual comes from the design
 * system ([MeshaColors] / [MeshaDimens] / [MeshaType] + [MeshaPrimaryButton] /
 * [MeshaInputShell]); no colours, raw dp/sp, or local token object here.
 *
 * Pre-session screen: email/password/language are LOCAL hoisted state; the only external
 * contract is [onSignIn]. Real credential verification (Firebase/backend) lands with the
 * auth pass; for now Sign-in is enabled once a valid email + a non-empty password are entered.
 */
@Composable
fun LoginScreen(
    onSignIn: (email: String) -> Unit,
    modifier: Modifier = Modifier,
    errorMessage: String? = null,
) {
    var email by remember { mutableStateOf("") }
    var password by remember { mutableStateOf("") }

    LoginContent(
        email = email,
        password = password,
        errorMessage = errorMessage,
        onEmailChange = { email = it },
        onPasswordChange = { password = it },
        onSignIn = { if (isValidEmail(email) && password.isNotBlank()) onSignIn(email.trim()) },
        modifier = modifier,
    )
}

private fun isValidEmail(raw: String): Boolean {
    val e = raw.trim()
    val at = e.indexOf('@')
    return at > 0 && at < e.length - 1 && e.substring(at + 1).contains('.')
}

@Composable
private fun LoginContent(
    email: String,
    password: String,
    onEmailChange: (String) -> Unit,
    onPasswordChange: (String) -> Unit,
    onSignIn: () -> Unit,
    modifier: Modifier = Modifier,
    errorMessage: String? = null,
) {
    val canSignIn = isValidEmail(email) && password.isNotBlank()
    val currentTag = LocalAppLanguage.current
    var showLangSheet by remember { mutableStateOf(false) }
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
            .verticalScroll(rememberScrollState())
            .padding(horizontal = MeshaDimens.gutter),
    ) {
        Spacer(Modifier.height(MeshaDimens.space7))
        BrandLockup()

        Spacer(Modifier.height(MeshaDimens.space8))
        FieldLabel("Work email")
        EmailField(email = email, onEmailChange = onEmailChange)

        Spacer(Modifier.height(MeshaDimens.space4))
        FieldLabel("Password")
        PasswordField(password = password, onPasswordChange = onPasswordChange, onDone = onSignIn)

        Spacer(Modifier.height(MeshaDimens.space5))
        MeshaPrimaryButton(text = "Sign in", enabled = canSignIn, onClick = onSignIn)

        Spacer(Modifier.height(MeshaDimens.space3))
        Centered("Use your Mesha work email and password.")

        Spacer(Modifier.height(MeshaDimens.space6))
        FieldLabel("App language")
        LanguageField(language = AppLocaleState.labelFor(currentTag), onClick = { showLangSheet = true })

        Spacer(Modifier.height(18.dp))
        Text(
            text = "Your role comes from the HR directory — you land on the screen for " +
                "your job. The field operator executes; managers and directors track.",
            color = MeshaColors.Faint,
            style = MeshaType.caption.copy(fontWeight = FontWeight.W500),
            lineHeight = 18.sp,
        )

        if (!errorMessage.isNullOrBlank()) {
            Spacer(Modifier.height(MeshaDimens.space5))
            Text(
                text = errorMessage,
                color = MeshaColors.Danger,
                style = MeshaType.cardSubtitle.copy(fontWeight = FontWeight.W600),
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
private fun EmailField(email: String, onEmailChange: (String) -> Unit) {
    BasicTextField(
        value = email,
        onValueChange = onEmailChange,
        singleLine = true,
        textStyle = MeshaType.body.copy(color = MeshaColors.Ink),
        cursorBrush = SolidColor(MeshaColors.Brand),
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email, imeAction = ImeAction.Next),
        modifier = Modifier.fillMaxWidth(),
        decorationBox = { inner ->
            MeshaInputShell {
                Icon(MeshaIcons.User, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(MeshaDimens.iconMd))
                Box(Modifier.weight(1f)) {
                    if (email.isEmpty()) Text("you@mesha.sg", color = MeshaColors.Faint, style = MeshaType.body)
                    inner()
                }
            }
        },
    )
}

@Composable
private fun PasswordField(password: String, onPasswordChange: (String) -> Unit, onDone: () -> Unit) {
    BasicTextField(
        value = password,
        onValueChange = onPasswordChange,
        singleLine = true,
        visualTransformation = PasswordVisualTransformation(),
        textStyle = MeshaType.body.copy(color = MeshaColors.Ink),
        cursorBrush = SolidColor(MeshaColors.Brand),
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, imeAction = ImeAction.Done),
        keyboardActions = KeyboardActions(onDone = { onDone() }),
        modifier = Modifier.fillMaxWidth(),
        decorationBox = { inner ->
            MeshaInputShell {
                Box(Modifier.weight(1f)) {
                    if (password.isEmpty()) Text("Your password", color = MeshaColors.Faint, style = MeshaType.body)
                    inner()
                }
            }
        },
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
        Text("Change", color = MeshaColors.Faint, style = MeshaType.cardSubtitle.copy(fontSize = 13.sp))
        Icon(MeshaIcons.Chevron, contentDescription = null, tint = MeshaColors.Faint, modifier = Modifier.size(13.dp))
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
                .background(MeshaColors.BrandTint)
                .border(2.dp, MeshaColors.Brand, CircleShape),
            contentAlignment = Alignment.Center,
        ) {
            // Mesha brand mark = Devanagari "मे" (mock `.logo`), NOT a Latin "M".
            Text(text = "मे", color = MeshaColors.Brand, fontSize = 16.sp, fontWeight = FontWeight.W800)
        }
        Spacer(Modifier.width(13.dp))
        Column {
            Text(text = "Mesha", color = MeshaColors.Ink, style = MeshaType.screenTitle.copy(fontWeight = FontWeight.W900, letterSpacing = (-0.66).sp))
            Text(text = "Field operations", color = MeshaColors.Faint, style = MeshaType.cardSubtitle.copy(fontSize = 12.sp, fontWeight = FontWeight.W600))
        }
    }
}

@Preview(name = "Login", showBackground = true, backgroundColor = 0xFF0A0F0C, widthDp = 380, heightDp = 820)
@Composable
private fun LoginPreview() {
    GoatOsTheme {
        LoginContent(
            email = "arun.kumar@mesha.sg",
            password = "hunter2",
            onEmailChange = {},
            onPasswordChange = {},
            onSignIn = {},
        )
    }
}
