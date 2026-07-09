package sg.mesha.goatos.device.camera

// Camera capture stays isolated behind this port (TRD: vendor/platform SDKs live
// only in device-*). The fake lets features + previews run without hardware.

interface CameraCapturePort {
    fun capture(): String
}

class FakeCameraCapture : CameraCapturePort {
    override fun capture(): String = ""
}
