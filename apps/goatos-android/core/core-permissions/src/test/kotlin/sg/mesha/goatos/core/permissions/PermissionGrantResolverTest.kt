package sg.mesha.goatos.core.permissions

import org.junit.Assert.assertEquals
import org.junit.Test

class PermissionGrantResolverTest {

    @Test
    fun `granted wins regardless of request history`() {
        assertEquals(
            PermissionGrantState.GRANTED,
            PermissionGrantResolver.resolve(isGranted = true, hasRequestedOnce = false, shouldShowRationale = false),
        )
        assertEquals(
            PermissionGrantState.GRANTED,
            PermissionGrantResolver.resolve(isGranted = true, hasRequestedOnce = true, shouldShowRationale = true),
        )
    }

    @Test
    fun `never requested and not granted is DENIED, not permanently denied`() {
        // Before any request, shouldShowRationale is also false — this must NOT be
        // misclassified as permanently denied ("never asked" vs "blocked").
        assertEquals(
            PermissionGrantState.DENIED,
            PermissionGrantResolver.resolve(isGranted = false, hasRequestedOnce = false, shouldShowRationale = false),
        )
    }

    @Test
    fun `denied once with rationale allowed can ask again`() {
        assertEquals(
            PermissionGrantState.DENIED,
            PermissionGrantResolver.resolve(isGranted = false, hasRequestedOnce = true, shouldShowRationale = true),
        )
    }

    @Test
    fun `denied after a real request with no rationale left is permanently denied`() {
        assertEquals(
            PermissionGrantState.PERMANENTLY_DENIED,
            PermissionGrantResolver.resolve(isGranted = false, hasRequestedOnce = true, shouldShowRationale = false),
        )
    }
}
