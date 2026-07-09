package sg.mesha.goatos

import android.os.Bundle
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
                    val signInError by sessionViewModel.signInError.collectAsStateWithLifecycle()
                    // Email-always sign-in; real OTP/Firebase verification is gated.
                    LoginScreen(
                        onSignIn = sessionViewModel::signIn,
                        errorMessage = signInError,
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
            }
        }
    }
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
