package sg.mesha.goatos

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.compose.runtime.getValue
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dagger.hilt.android.AndroidEntryPoint
import sg.mesha.goatos.boot.BootstrapViewModel
import sg.mesha.goatos.boot.SessionViewModel
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.feature.auth.LoginScreen
import sg.mesha.goatos.ui.GoatOsShell

@AndroidEntryPoint
class MainActivity : ComponentActivity() {

    private val bootstrapViewModel: BootstrapViewModel by viewModels()
    private val sessionViewModel: SessionViewModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            GoatOsTheme {
                val authed by sessionViewModel.isAuthed.collectAsStateWithLifecycle()
                if (!authed) {
                    // Email-always sign-in; real OTP/Firebase verification is gated.
                    LoginScreen(onSignIn = sessionViewModel::signIn)
                } else {
                    val navState by bootstrapViewModel.navState.collectAsStateWithLifecycle()
                    GoatOsShell(navState = navState)
                }
            }
        }
    }
}
