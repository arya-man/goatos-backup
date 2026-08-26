package sg.mesha.goatos.capture

import android.graphics.Bitmap
import sg.mesha.goatos.R

internal enum class ProofTorchMode {
    AUTO,
    ON,
    OFF,
}

internal fun ProofTorchMode.next(): ProofTorchMode = when (this) {
    ProofTorchMode.AUTO -> ProofTorchMode.ON
    ProofTorchMode.ON -> ProofTorchMode.OFF
    ProofTorchMode.OFF -> ProofTorchMode.AUTO
}

internal fun proofTorchEnabled(
    mode: ProofTorchMode,
    lowLight: Boolean,
    hasFlashUnit: Boolean,
): Boolean {
    if (!hasFlashUnit) return false
    return when (mode) {
        ProofTorchMode.AUTO -> lowLight
        ProofTorchMode.ON -> true
        ProofTorchMode.OFF -> false
    }
}

internal fun proofTorchLabelRes(mode: ProofTorchMode, torchEnabled: Boolean): Int = when (mode) {
    ProofTorchMode.AUTO -> if (torchEnabled) {
        R.string.proof_camera_flash_auto_on
    } else {
        R.string.proof_camera_flash_auto
    }
    ProofTorchMode.ON -> R.string.proof_camera_flash_on
    ProofTorchMode.OFF -> R.string.proof_camera_flash_off
}

internal fun isLowLightAverageLuma(
    totalLuma: Long,
    samples: Int,
    threshold: Double = LOW_LIGHT_LUMA_THRESHOLD,
): Boolean = samples > 0 && totalLuma.toDouble() / samples.toDouble() < threshold

internal fun isLowLightPreview(bitmap: Bitmap?): Boolean {
    if (bitmap == null || bitmap.width <= 0 || bitmap.height <= 0) return false
    val stepX = (bitmap.width / 24).coerceAtLeast(1)
    val stepY = (bitmap.height / 24).coerceAtLeast(1)
    var totalLuma = 0L
    var samples = 0
    var y = stepY / 2
    while (y < bitmap.height) {
        var x = stepX / 2
        while (x < bitmap.width) {
            val pixel = bitmap.getPixel(x, y)
            val red = (pixel shr 16) and 0xff
            val green = (pixel shr 8) and 0xff
            val blue = pixel and 0xff
            totalLuma += ((red * 299) + (green * 587) + (blue * 114)) / 1000
            samples += 1
            x += stepX
        }
        y += stepY
    }
    return isLowLightAverageLuma(totalLuma, samples)
}

private const val LOW_LIGHT_LUMA_THRESHOLD = 48.0
