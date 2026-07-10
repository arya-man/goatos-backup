package sg.mesha.goatos.feature.auth

/**
 * Sign-in failure reasons [LoginScreen] can render as localized messages. This lives in
 * feature-auth so the enum and strings stay in the same module.
 */
enum class LoginError {
    INVALID_CREDENTIALS,
    NETWORK,
    TOO_MANY_REQUESTS,
    GOOGLE_CANCELLED,
    NO_GOOGLE_ACCOUNT,
    NO_DEV_BACKEND,
    UNKNOWN,
}
