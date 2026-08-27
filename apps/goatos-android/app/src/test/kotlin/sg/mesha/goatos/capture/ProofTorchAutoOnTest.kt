package sg.mesha.goatos.capture

import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Pins the two facts that make the in-app recorder's light usable in a dark shed.
 *
 * Operators film proof at dusk and inside sheds, and the torch used to start OFF: every clip
 * needed a deliberate tap on a control most operators never found, so proofs came back too dark
 * for a verifier to judge. The light now comes on WITH the recording, and the operator only ever
 * has to turn it off.
 *
 * The second fact is the one a future edit is most likely to break: turning the torch off must
 * NOT interrupt the clip in flight. The torch is a camera CONTROL (`enableTorch`); re-binding the
 * camera to change the light would tear down the active recording and lose the proof.
 *
 * Deliberately NOT asserted here: that a real phone's LED lights. A JVM test cannot drive CameraX,
 * and a configuration assertion is not a recorded file — device proof belongs in the handoff.
 * This follows the same source-assertion shape as [ProofAudioCaptureTest], for the same reason.
 */
class ProofTorchAutoOnTest {

    @Test
    fun `the recorder lights the torch when recording starts`() {
        val text = recorderSource()

        assertTrue(
            "startRecording() must switch the torch on once the clip is running. Without it the " +
                "operator has to find the flash control before every recording, which is how " +
                "unreadably dark shed proofs reached the verifier queue.",
            AUTO_ON_AT_RECORD_START.containsMatchIn(text),
        )
        assertTrue(
            "The auto-on must go through applyTorch(), which checks hasFlashUnit() first — a " +
                "phone with no flash unit stays dark instead of throwing at record time.",
            text.contains("if (!camera.cameraInfo.hasFlashUnit()) return"),
        )
    }

    @Test
    fun `switching the torch off never touches the recording in flight`() {
        val text = recorderSource()
        val body = APPLY_TORCH_BODY.find(text)

        assertNotNull(
            "applyTorch() is the single place the torch is switched; it must stay that way so " +
                "the light, the control state and the analytics event cannot disagree.",
            body,
        )
        val helper = body!!.value

        assertTrue(
            "applyTorch() must change the light through cameraControl.enableTorch().",
            helper.contains("cameraControl.enableTorch(next)"),
        )
        for (forbidden in RECORDING_TOUCHING_CALLS) {
            assertTrue(
                "applyTorch() must not $forbidden. The torch is a camera control, not a rebind: " +
                    "an operator turning the light off mid-clip must keep recording, and " +
                    "unbinding or stopping here would lose the proof they are shooting.",
                !helper.contains(forbidden),
            )
        }
    }

    /** Walks up from the test's working directory so the test is independent of the Gradle CWD. */
    private fun recorderSource(): String {
        var dir: File? = File("").absoluteFile
        while (dir != null) {
            val candidate = File(dir, "app/src/main/kotlin/$RECORDER_PATH")
            if (candidate.isFile) return candidate.readText()
            val self = File(dir, "src/main/kotlin/$RECORDER_PATH")
            if (self.isFile) return self.readText()
            dir = dir.parentFile
        }
        throw AssertionError("Could not locate InAppVideoRecorder.kt from ${File("").absolutePath}")
    }

    private companion object {
        const val RECORDER_PATH = "sg/mesha/goatos/capture/InAppVideoRecorder.kt"

        /** `isRecording = true` … then the auto-on, tolerating the comment lines between them. */
        val AUTO_ON_AT_RECORD_START = Regex(
            """isRecording\s*=\s*true[\s\S]{0,400}?applyTorch\(true\)""",
        )

        /** The body of `fun applyTorch(...)`, up to the closing brace at its own indent. */
        val APPLY_TORCH_BODY = Regex(
            """fun applyTorch\([\s\S]*?\n    \}""",
        )

        val RECORDING_TOUCHING_CALLS = listOf(
            "activeRecording",
            "cameraSession.bind",
            "cameraSession.release",
            "retryGeneration",
        )
    }
}
