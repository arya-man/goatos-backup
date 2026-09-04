package sg.mesha.goatos

sealed interface ForceUpdateAttemptState {
    data object Idle : ForceUpdateAttemptState
    data object Downloading : ForceUpdateAttemptState
    data object PermissionNeeded : ForceUpdateAttemptState
    data object InstallerOpened : ForceUpdateAttemptState
    data class Failed(val reason: String) : ForceUpdateAttemptState
}
