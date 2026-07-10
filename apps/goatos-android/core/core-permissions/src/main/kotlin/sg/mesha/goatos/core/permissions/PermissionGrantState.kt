package sg.mesha.goatos.core.permissions

/**
 * Tri-state grant outcome for one [AppPermission] — the generic Android permission
 * status matrix (granted / denied-can-ask-again / permanently-denied). This is
 * distinct from the RFID reader's own READY/PAIRED_NOT_READY/... connectivity matrix
 * (docs/mobile/rfid-keyboard-reader.md), which is a device-rfid concern; this module
 * only tracks the OS permission grant, not hardware pairing/input-device state.
 */
enum class PermissionGrantState {
    GRANTED,
    DENIED,
    PERMANENTLY_DENIED,
}

/** One row of the login-time permission status matrix. */
data class PermissionStatus(
    val permission: AppPermission,
    val grantState: PermissionGrantState,
)
