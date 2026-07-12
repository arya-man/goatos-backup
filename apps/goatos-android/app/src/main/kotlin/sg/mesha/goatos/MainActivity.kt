package sg.mesha.goatos

import android.os.Bundle
import android.view.KeyEvent
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dagger.hilt.android.AndroidEntryPoint
import sg.mesha.goatos.boot.BootstrapUiState
import sg.mesha.goatos.boot.BootstrapViewModel
import sg.mesha.goatos.boot.SessionViewModel
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import kotlinx.coroutines.flow.first
import sg.mesha.goatos.core.datastore.SessionStore
import sg.mesha.goatos.core.designsystem.locale.AppLocaleState
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.feature.auth.LoginScreen
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.ui.GoatOsShell
import javax.inject.Inject

@AndroidEntryPoint
class MainActivity : ComponentActivity() {

    private val bootstrapViewModel: BootstrapViewModel by viewModels()
    private val sessionViewModel: SessionViewModel by viewModels()

    /** V1 keyboard-wedge RFID reader — captures hardware tag reads at the activity layer. */
    @Inject
    lateinit var rfidReader: RfidReaderPort

    /** Persisted app language — restored on launch, saved when the picker changes it. */
    @Inject
    lateinit var sessionStore: SessionStore

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            GoatOsTheme {
                // Restore the saved language once, and persist any picker change app-wide.
                LaunchedEffect(Unit) { runCatching { AppLocaleState.set(sessionStore.language.first()) } }
                LaunchedEffect(AppLocaleState.tag) { runCatching { sessionStore.setLanguage(AppLocaleState.tag) } }
                ProvideAppLocale {
                val authed by sessionViewModel.isAuthed.collectAsStateWithLifecycle()
                // Logout clean-slate (C35-001): BootstrapViewModel is Activity-scoped, so it
                // otherwise survives a logout with the departing user's Ready(navState) still
                // held. `null` marks "not yet observed" (first composition, e.g. app cold start
                // with an existing session) so the ViewModel's own initial load isn't duplicated;
                // an actual false -> true edge (a fresh sign-in after a logout, same Activity)
                // forces a real reload instead of ever rendering stale nav/identity.
                var previousAuthed by remember { mutableStateOf<Boolean?>(null) }
                LaunchedEffect(authed) {
                    val wasAuthed = previousAuthed
                    previousAuthed = authed
                    when {
                        !authed -> bootstrapViewModel.reset()
                        wasAuthed == false -> bootstrapViewModel.load()
                    }
                }
                if (!authed) {
                    val uiState by sessionViewModel.uiState.collectAsStateWithLifecycle()
                    // dev flavor: local HS256 bearer; stg/prod: real Firebase Auth.
                    // LoginScreen itself renders the login-time device-permission gate
                    // (Camera/Bluetooth/Notifications — core-permissions + rationale UI);
                    // it supersedes the old onCreate-time silent BLUETOOTH_CONNECT request.
                    LoginScreen(
                        onSignInEmail = sessionViewModel::signInWithEmail,
                        onGoogle = { sessionViewModel.signInWithGoogle(this@MainActivity) },
                        onForgotPassword = sessionViewModel::sendPasswordReset,
                        isLoading = uiState.isLoading,
                        errorReason = uiState.errorReason,
                        errorDetail = uiState.errorDetail,
                        resetEmailSent = uiState.resetEmailSent,
                    )
                } else {
                    val bootstrap by bootstrapViewModel.state.collectAsStateWithLifecycle()
                    when (val s = bootstrap) {
                        BootstrapUiState.Loading -> BootstrapLoading()
                        is BootstrapUiState.Ready -> GoatOsShell(navState = s.navState)
                        is BootstrapUiState.Error ->
                            BootstrapError(message = s.message, onRetry = bootstrapViewModel::load)
                    }
                }
                } // ProvideAppLocale
            }
        }
    }

    override fun onResume() {
        super.onResume()
        rfidReader.refreshStatus()
    }

    /**
     * Route every hardware key event through the RFID capture first. A keyboard-wedge
     * reader types the tag as key events + Enter; the capture consumes them only while an
     * RFID-accepting screen enabled capture, so normal typing/navigation is unaffected.
     */
    override fun dispatchKeyEvent(event: KeyEvent): Boolean =
        rfidReader.onKeyEvent(event) || super.dispatchKeyEvent(event)
}

@Composable
private fun BootstrapLoading() {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        CircularProgressIndicator(color = MaterialTheme.colorScheme.primary)
    }
}

@Composable
private fun BootstrapError(message: String, onRetry: () -> Unit) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .padding(32.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = message,
            color = MaterialTheme.colorScheme.onBackground,
            fontSize = 14.sp,
            textAlign = TextAlign.Center,
        )
        Text(
            text = "Retry",
            color = MaterialTheme.colorScheme.primary,
            fontSize = 14.sp,
            fontWeight = FontWeight.Bold,
            textAlign = TextAlign.Center,
            modifier = Modifier
                .padding(top = 20.dp)
                .clip(RoundedCornerShape(10.dp))
                .clickable(onClick = onRetry)
                .padding(horizontal = 24.dp, vertical = 10.dp),
        )
    }
}
