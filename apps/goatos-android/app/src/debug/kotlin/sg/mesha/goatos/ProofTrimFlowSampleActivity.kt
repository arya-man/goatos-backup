package sg.mesha.goatos

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import dagger.hilt.android.AndroidEntryPoint
import kotlinx.coroutines.launch
import sg.mesha.goatos.capture.BindVideoCaptureSource
import sg.mesha.goatos.capture.CaptureAccessGate
import sg.mesha.goatos.capture.ProofCaptureContext
import sg.mesha.goatos.capture.ProofCapturePrompt
import sg.mesha.goatos.capture.rememberDelegatingProofCaptureSource
import sg.mesha.goatos.core.proofedit.ProofEditGate

/**
 * DEBUG-ONLY harness for the capture → trim → join flow.
 *
 * Exists because verifying the wired flow on a phone otherwise needs the whole phone-QA stack
 * (backend, throwaway database, login, a real packing task) before the camera can even be opened —
 * so the editor's device behaviour would go unverified until all of that is standing up.
 *
 * It drives the REAL production composables (`BindVideoCaptureSource` → `ProofCaptureFlow` →
 * `ProofTrimEditor` → `ProofVideoStitcher`), not a copy of them. The only thing it fakes is the
 * bootstrap flag, which it forces on locally so the gate opens without a backend.
 *
 * Debug source set only: it is absent from release builds entirely, and it never writes a proof
 * row, never enqueues an upload, and never reaches the backend.
 *
 * Launch:
 * `adb shell am start -n sg.mesha.goatos.debug/sg.mesha.goatos.ProofTrimFlowSampleActivity`
 */
@AndroidEntryPoint
class ProofTrimFlowSampleActivity : ComponentActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            MaterialTheme {
                val source = rememberDelegatingProofCaptureSource()
                val scope = rememberCoroutineScope()
                var result by remember { mutableStateOf("No capture yet") }

                CaptureAccessGate {
                    // Flag forced on so the gate opens without bootstrap; the SURFACE check is the
                    // real one, so this still proves feed packing (and only feed packing) opens the
                    // editor.
                    BindVideoCaptureSource(
                        source = source,
                        featureFlags = mapOf(ProofEditGate.FLAG_KEY to true),
                        onTelemetry = { event, props -> result = "$event $props" },
                    )

                    Column(
                        Modifier
                            .fillMaxSize()
                            .padding(24.dp),
                        verticalArrangement = Arrangement.Center,
                        horizontalAlignment = Alignment.CenterHorizontally,
                    ) {
                        Text("Capture flow harness", style = MaterialTheme.typography.titleLarge)
                        Spacer(Modifier.height(20.dp))
                        Button(
                            modifier = Modifier.fillMaxWidth(),
                            onClick = {
                                scope.launch {
                                    val captured = source.captureVideo(
                                        ProofCaptureContext(
                                            title = "Feed packing",
                                            primaryTag = "Castro - 2",
                                            workLabel = "Session 1",
                                            prompt = ProofCapturePrompt.FEED_PACKING,
                                            featureSurface = "feed_packing",
                                        ),
                                    )
                                    result = captured?.localUri ?: "cancelled"
                                }
                            },
                        ) { Text("Record feed packing (editor ON)") }

                        Spacer(Modifier.height(12.dp))
                        Button(
                            modifier = Modifier.fillMaxWidth(),
                            onClick = {
                                scope.launch {
                                    val captured = source.captureVideo(
                                        ProofCaptureContext(
                                            title = "Vaccination",
                                            primaryTag = "982000123456789",
                                            prompt = ProofCapturePrompt.VACCINATION,
                                            featureSurface = "vaccination",
                                        ),
                                    )
                                    result = captured?.localUri ?: "cancelled"
                                }
                            },
                        ) { Text("Record vaccination (editor must NOT appear)") }

                        Spacer(Modifier.height(20.dp))
                        Text(result, style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
        }
    }
}
