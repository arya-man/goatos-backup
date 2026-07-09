package sg.mesha.goatos.core.ui

import androidx.compose.material3.Text
import androidx.compose.runtime.Composable

// Shared, stateless UI primitives for feature-* screens. Placeholders — refine
// against the mock in the design-system pass.

@Composable
fun SectionHeader(title: String) {
    Text(title)
}

@Composable
fun EmptyState(message: String) {
    Text(message)
}
