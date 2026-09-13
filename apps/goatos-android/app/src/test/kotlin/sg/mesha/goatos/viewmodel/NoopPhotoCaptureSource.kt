package sg.mesha.goatos.viewmodel

import sg.mesha.goatos.capture.CapturedPhoto
import sg.mesha.goatos.capture.PhotoCaptureContext
import sg.mesha.goatos.capture.PhotoCaptureSource

/** A photo camera that never captures (the operator cancelled), for ViewModel tests. */
internal class NoopPhotoCaptureSource : PhotoCaptureSource {
    override suspend fun capturePhoto(context: PhotoCaptureContext): CapturedPhoto? = null
}
