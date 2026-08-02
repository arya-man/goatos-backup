package sg.mesha.goatos.core.permissions

import org.junit.Assert.assertEquals
import org.junit.Test

class PermissionGrantResolverTest {

    @Test
    fun `granted wins regardless of request history`() {
        assertEquals(
            PermissionGrantState.GRANTED,
            PermissionGrantResolver.resolve(isGranted = true, requestCount = 0, shouldShowRationale = false),
        )
        assertEquals(
            PermissionGrantState.GRANTED,
            PermissionGrantResolver.resolve(isGranted = true, requestCount = 5, shouldShowRationale = true),
        )
    }

    @Test
    fun `never requested and not granted is DENIED, not permanently denied`() {
        // Before any request, shouldShowRationale is also false — this must NOT be
        // misclassified as permanently denied ("never asked" vs "blocked").
        assertEquals(
            PermissionGrantState.DENIED,
            PermissionGrantResolver.resolve(isGranted = false, requestCount = 0, shouldShowRationale = false),
        )
    }

    @Test
    fun `denied once with rationale allowed can ask again`() {
        assertEquals(
            PermissionGrantState.DENIED,
            PermissionGrantResolver.resolve(isGranted = false, requestCount = 1, shouldShowRationale = true),
        )
    }

    @Test
    fun `denied once with no rationale is still retryable, not permanently denied`() {
        // Xiaomi/MIUI (and HyperOS) commonly report shouldShowRequestPermissionRationale
        // = false after a single ordinary denial, and can auto-deny outright. Treating
        // that as "blocked" dead-ends the operator on an Open-settings screen after ONE
        // denial, with the OS never having set USER_FIXED. One denial must always be
        // retryable by asking again.
        assertEquals(
            PermissionGrantState.DENIED,
            PermissionGrantResolver.resolve(isGranted = false, requestCount = 1, shouldShowRationale = false),
        )
    }

    @Test
    fun `denied twice with no rationale left is permanently denied`() {
        assertEquals(
            PermissionGrantState.PERMANENTLY_DENIED,
            PermissionGrantResolver.resolve(isGranted = false, requestCount = 2, shouldShowRationale = false),
        )
    }

    @Test
    fun `denied twice but rationale still offered can ask again`() {
        // Stock Android keeps rationale=true while the OS is still willing to prompt.
        assertEquals(
            PermissionGrantState.DENIED,
            PermissionGrantResolver.resolve(isGranted = false, requestCount = 2, shouldShowRationale = true),
        )
    }

    @Test
    fun `many denials with no rationale stay permanently denied`() {
        assertEquals(
            PermissionGrantState.PERMANENTLY_DENIED,
            PermissionGrantResolver.resolve(isGranted = false, requestCount = 7, shouldShowRationale = false),
        )
    }
}
