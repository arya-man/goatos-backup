package sg.mesha.goatos

import android.annotation.SuppressLint
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.view.KeyEvent
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.runtime.CompositionLocalProvider
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
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dagger.hilt.android.AndroidEntryPoint
import sg.mesha.goatos.boot.BootstrapErrorType
import sg.mesha.goatos.boot.BootstrapUiState
import sg.mesha.goatos.boot.BootstrapViewModel
import sg.mesha.goatos.boot.SessionViewModel
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import kotlinx.coroutines.flow.first
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsSession
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.datastore.SessionStore
import sg.mesha.goatos.core.designsystem.locale.AppLocaleState
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.media.LocalProofPlayerFactory
import sg.mesha.goatos.core.media.ProofPlayerFactory
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.feature.auth.LoginScreen
import sg.mesha.goatos.push.PendingNavigation
import sg.mesha.goatos.push.PushExtras
import sg.mesha.goatos.push.resolvePushRoute
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.ui.ForceUpdateScreen
import sg.mesha.goatos.ui.GoatOsShell
import sg.mesha.goatos.update.UpdateGateUiState
import sg.mesha.goatos.update.UpdateGateViewModel
import javax.inject.Inject

@AndroidEntryPoint
class MainActivity : ComponentActivity() {

    private val bootstrapViewModel: BootstrapViewModel by viewModels()
    private val sessionViewModel: SessionViewModel by viewModels()
    private val updateGateViewModel: UpdateGateViewModel by viewModels()

    /** V1 keyboard-wedge RFID reader — captures hardware tag reads at the activity layer. */
    @Inject
    lateinit var rfidReader: RfidReaderPort

    /** Persisted app language — restored on launch, saved when the picker changes it. */
    @Inject
    lateinit var sessionStore: SessionStore

    /** Holds a notification-tap route until GoatOsShell's NavHost exists to consume it — see
     *  [PendingNavigation]'s KDoc for why a tap can arrive before that NavHost is composed. */
    @Inject
    lateinit var pendingNavigation: PendingNavigation

    /** Builds proof-video players over the telemetry-instrumented OkHttp client (W-22). */
    @Inject
    lateinit var proofPlayerFactory: ProofPlayerFactory

    /** Session-boundary + force-update-gate analytics owned directly by this Activity (app
     *  open/backgrounded and the force-update gate both render here, above any ViewModel that
     *  already holds an [AnalyticsPort]). */
    @Inject
    lateinit var analytics: AnalyticsPort

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        handlePushIntent(intent)
        setContent {
            GoatOsTheme {
                // Restore the saved language once, and persist any picker change app-wide.
                LaunchedEffect(Unit) { runCatching { AppLocaleState.set(sessionStore.language.first()) } }
                LaunchedEffect(AppLocaleState.tag) { runCatching { sessionStore.setLanguage(AppLocaleState.tag) } }
                ProvideAppLocale {
                // Proof-video players below this point fetch over the app's instrumented OkHttp
                // client, so a 403/404/500 on a signed playback URL produces the same logcat +
                // Crashlytics + api_call_failure signal a failed API call does (W-22).
                CompositionLocalProvider(LocalProofPlayerFactory provides proofPlayerFactory) {
                // Force-update gate sits ABOVE auth + bootstrap: an out-of-date build is
                // blocked whether or not anyone is signed in. Fails open, so an
                // unconfigured environment (e.g. the dev flavor) renders the app normally.
                val updateGate by updateGateViewModel.state.collectAsStateWithLifecycle()
                when (val gate = updateGate) {
                    UpdateGateUiState.Checking -> BootstrapLoading()

                    is UpdateGateUiState.Blocked -> {
                        // Fires once per distinct block this process sees (a later refresh that
                        // re-confirms the SAME block must not re-fire "shown" — see
                        // FORCE_UPDATE_GATE_BLOCKING for the "still blocked" signal, driven from
                        // onResume() instead). This gate sits above auth, so this LaunchedEffect
                        // is the only place its state is ever visible in analytics at all.
                        LaunchedEffect(gate.updateUrl) {
                            analytics.track(AnalyticsEventsSession.FORCE_UPDATE_GATE_SHOWN)
                        }
                        ForceUpdateScreen(
                            updateUrl = gate.updateUrl,
                            installedVersionName = BuildConfig.VERSION_NAME,
                            onUpdate = { url ->
                                analytics.track(AnalyticsEventsSession.FORCE_UPDATE_TAPPED)
                                openExternalUrl(url)
                            },
                        )
                    }

                    UpdateGateUiState.Allowed -> {
                val authed by sessionViewModel.isAuthed.collectAsStateWithLifecycle()
                // Logout clean-slate (C35-001): BootstrapViewModel is Activity-scoped, so it
                // otherwise survives a logout with the departing user's Ready(navState) still
                // held. BootstrapViewModel also starts before the auth gate has resolved, so the
                // first authenticated state must reload from the backend with the now-authoritative
                // bearer instead of trusting any pre-auth/cached bootstrap. That is what keeps a
                // dev APK reinstalled with an operator token from rendering a stale leadership
                // shell that was loaded before the new session became active.
                var previousAuthed by remember { mutableStateOf<Boolean?>(null) }
                LaunchedEffect(authed) {
                    val wasAuthed = previousAuthed
                    previousAuthed = authed
                    when {
                        authed == null -> Unit // session not read yet; decide nothing
                        authed == false -> bootstrapViewModel.reset()
                        wasAuthed != true -> {
                            bootstrapViewModel.reset()
                            bootstrapViewModel.load()
                        }
                    }
                }
                if (authed == null) {
                    // Session not read yet: show neither the app nor the login gate.
                    BootstrapLoading()
                } else if (authed == false) {
                    val uiState by sessionViewModel.uiState.collectAsStateWithLifecycle()
                    // dev flavor: local HS256 bearer; stg/prod: real Firebase Auth.
                    // LoginScreen itself renders the login-time device-permission gate
                    // (Camera/Bluetooth/Notifications — core-permissions + rationale UI);
                    // it supersedes the old onCreate-time silent BLUETOOTH_CONNECT request.
                    LoginScreen(
                        onSignInEmail = sessionViewModel::signInWithEmail,
                        onGoogle = { sessionViewModel.signInWithGoogle(this@MainActivity) },
                        onForgotPassword = sessionViewModel::sendPasswordReset,
                        appVersionLabel = appVersionLabel(),
                        isLoading = uiState.isLoading,
                        errorReason = uiState.errorReason,
                        errorDetail = uiState.errorDetail,
                        resetEmailSent = uiState.resetEmailSent,
                        onPermissionGateShown = { missingPermissions ->
                            missingPermissions.forEach { permission ->
                                analytics.track(
                                    AnalyticsEventsSession.PERMISSION_GATE_SHOWN,
                                    mapOf(AnalyticsEventsSession.Params.PERMISSION to permission),
                                )
                            }
                        },
                        onPermissionAnswered = { permission, granted ->
                            analytics.track(
                                AnalyticsEventsSession.PERMISSION_GATE_RESULT,
                                mapOf(
                                    AnalyticsEventsSession.Params.PERMISSION to permission,
                                    AnalyticsEvents.Params.REASON to if (granted) "granted" else "denied",
                                ),
                            )
                        },
                    )
                } else {
                    val bootstrap by bootstrapViewModel.state.collectAsStateWithLifecycle()
                    when (val s = bootstrap) {
                        BootstrapUiState.Loading -> BootstrapLoading()
                        is BootstrapUiState.Ready -> GoatOsShell(navState = s.navState)
                        is BootstrapUiState.Error -> {
                            when (s.errorType) {
                                BootstrapErrorType.AUTH_SESSION_EXPIRED -> {
                                    // Auth failure: sign out and return to login screen. This is
                                    // the user-visible boundary of a forced re-auth — the moment
                                    // a still-open app becomes a login gate again, not merely the
                                    // underlying bootstrap_failed(reason=AuthSessionExpired) which
                                    // fired before any UI reflected it.
                                    LaunchedEffect(Unit) {
                                        analytics.track(AnalyticsEventsSession.SESSION_TOKEN_EXPIRED)
                                    }
                                    BootstrapError(
                                        message = stringResource(R.string.bootstrap_error_auth_session_expired),
                                        actionLabel = stringResource(R.string.bootstrap_action_sign_in_again),
                                        onAction = { sessionViewModel.signOut() }
                                    )
                                }
                                BootstrapErrorType.ACCESS_NOT_PROVISIONED -> {
                                    // Valid sign-in, access not set up. Retry only: signing out
                                    // would wipe unsynced work and could not fix this.
                                    BootstrapError(
                                        message = stringResource(R.string.bootstrap_error_access_not_provisioned),
                                        actionLabel = stringResource(R.string.bootstrap_action_retry),
                                        onAction = bootstrapViewModel::load
                                    )
                                }
                                BootstrapErrorType.CONNECTIVITY_FAILURE -> {
                                    // Connectivity failure: show retryable error.
                                    BootstrapError(
                                        message = stringResource(R.string.bootstrap_error_connectivity),
                                        actionLabel = stringResource(R.string.bootstrap_action_retry),
                                        onAction = bootstrapViewModel::load
                                    )
                                }
                            }
                        }
                    }
                }
                    } // UpdateGateUiState.Allowed
                } // when (updateGate)
                } // CompositionLocalProvider(LocalProofPlayerFactory)
                } // ProvideAppLocale
            }
        }
    }

    override fun onResume() {
        super.onResume()
        rfidReader.refreshStatus()
        // Stop the cold-start custom trace (docs/TELEMETRY.md item 3) exactly once — the
        // Application-held handle is nulled after stopping so a later onResume (e.g. returning
        // from the background) never re-stops it.
        (application as? GoatOsApplication)?.let { app ->
            app.coldStartTrace?.stop()
            app.coldStartTrace = null
        }
        // Re-check the update floor on every foreground: a minimum raised while the app
        // was backgrounded blocks the build the next time it comes forward.
        updateGateViewModel.refresh()
        // A resume that finds the gate ALREADY blocked (not a fresh block first seen this
        // launch — that is FORCE_UPDATE_GATE_SHOWN, fired from the Compose branch below) means
        // the operator came back to the app without updating. Read synchronously off the
        // StateFlow's current value rather than a coroutine collector, so this never races the
        // Compose recomposition that renders the same state.
        if (updateGateViewModel.state.value is UpdateGateUiState.Blocked) {
            analytics.track(AnalyticsEventsSession.FORCE_UPDATE_GATE_BLOCKING)
        }
    }

    override fun onStop() {
        super.onStop()
        // Session-boundary: the app left the foreground. Pairs with APP_OPEN/SESSION_START
        // (GoatOsApplication.onCreate) so a session's visible span is reconstructible even when
        // it ends by backgrounding rather than an explicit sign-out.
        analytics.track(AnalyticsEventsSession.APP_BACKGROUNDED)
    }

    /**
     * A notification tap on an already-running Activity (`launchMode="singleTop"`, set in
     * AndroidManifest) delivers here instead of creating a new Activity instance — without
     * `singleTop` + this override, a second tap while the app is already open would either
     * stack a duplicate Activity or silently drop the new intent's extras.
     */
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handlePushIntent(intent)
    }

    /**
     * Reads a notification's routing extras (see [PushExtras.ROUTE_KEYS] — written by
     * [sg.mesha.goatos.push.PushNotifications]'s tap `PendingIntent`, OR by the OS itself for a
     * background/killed-app FCM auto-display tap) and resolves + stashes the target route in
     * [PendingNavigation]. A no-op for any intent that isn't a push tap (e.g. the plain
     * LAUNCHER intent) — [PushExtras.ROUTE_KEYS] all absent means nothing to route.
     */
    private fun handlePushIntent(intent: Intent?) {
        val extras = intent?.extras ?: return
        val payload = PushExtras.ROUTE_KEYS
            .mapNotNull { key -> extras.getString(key)?.takeIf { it.isNotBlank() }?.let { key to it } }
            .toMap()
        if (payload.isEmpty()) return
        // No recognisable destination is not an error and not a reason to pick a module: leaving
        // the pending route unset opens the app on this person's own home screen.
        resolvePushRoute(payload)?.let { pendingNavigation.set(it) }
    }

    /**
     * Opens the force-update install link (a Firebase App Distribution tester link) in the
     * browser / App Distribution app. New-task launch because it leaves the app; wrapped so
     * a missing handler never crashes the gate — the CTA simply no-ops.
     */
    private fun openExternalUrl(url: String) {
        if (url.isBlank()) return
        val intent = Intent(Intent.ACTION_VIEW, Uri.parse(url)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        // Swallow a missing handler (e.g. no browser) so the gate CTA never crashes the app.
        runCatching { startActivity(intent) }
    }

    /**
     * Route every hardware key event through the RFID capture before the Compose view tree.
     * A keyboard-wedge reader terminates a tag with Enter; intercepting only in
     * [onKeyDown]/[onKeyUp] is too late because a focused Compose control (for example the
     * Scan screen's Back affordance) may consume Enter during view dispatch first. When RFID
     * capture is inactive, or the key is unrelated to a tag, the event still follows the
     * normal Activity/View path unchanged.
     */
    // Activity.dispatchKeyEvent is the public platform interception point required for a
    // keyboard-wedge reader. ComponentActivity redeclares it with a library-group lint
    // restriction, so suppress that annotation only on this intentional platform override.
    @SuppressLint("RestrictedApi")
    override fun dispatchKeyEvent(event: KeyEvent): Boolean = dispatchRfidFirst(
        rfidConsumes = { rfidReader.onKeyEvent(event) },
        dispatchNormally = { super.dispatchKeyEvent(event) },
    )
}

/** Keeps the Activity's input-order contract independently regression-testable. */
internal fun dispatchRfidFirst(
    rfidConsumes: () -> Boolean,
    dispatchNormally: () -> Boolean,
): Boolean = if (rfidConsumes()) true else dispatchNormally()

private fun appVersionLabel(): String = "Version ${BuildConfig.VERSION_NAME} (code ${BuildConfig.VERSION_CODE})"

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
private fun BootstrapError(
    message: String,
    actionLabel: String,
    onAction: () -> Unit,
) {
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
            text = actionLabel,
            color = MaterialTheme.colorScheme.primary,
            fontSize = 14.sp,
            fontWeight = FontWeight.Bold,
            textAlign = TextAlign.Center,
            modifier = Modifier
                .padding(top = 20.dp)
                .minimumInteractiveComponentSize()
                .clip(RoundedCornerShape(10.dp))
                .clickable(onClick = onAction)
                .padding(horizontal = 24.dp, vertical = 10.dp),
        )
    }
}
