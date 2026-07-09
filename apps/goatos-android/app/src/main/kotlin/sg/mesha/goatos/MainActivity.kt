package sg.mesha.goatos

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import sg.mesha.goatos.boot.BootstrapViewModel
import sg.mesha.goatos.core.data.DefaultBootstrapRepository
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.network.FakeAppApi
import sg.mesha.goatos.ui.GoatOsShell

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            GoatOsTheme {
                // Skeleton wiring: a fake bootstrap drives the shell (chrome=expanded)
                // so the backend-driven nav renders end-to-end. Hilt + the real
                // Retrofit/OpenAPI client + auth replace this in the DI/network passes.
                val repo = remember { DefaultBootstrapRepository(FakeAppApi(chrome = "expanded")) }
                val viewModel: BootstrapViewModel = viewModel(factory = BootstrapViewModel.factory(repo))
                val navState by viewModel.navState.collectAsStateWithLifecycle()
                GoatOsShell(navState = navState)
            }
        }
    }
}
