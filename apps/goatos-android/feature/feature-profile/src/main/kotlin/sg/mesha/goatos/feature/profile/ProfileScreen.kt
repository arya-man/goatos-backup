package sg.mesha.goatos.feature.profile

import androidx.compose.material3.Text
import androidx.compose.runtime.Composable

// Per TRD: single-feature principals have no sidebar, so their extras — language,
// RFID reader pairing, notifications, and sign out — live here in the profile /
// "You" surface rather than a dedicated settings module.
@Composable
fun ProfileScreen() {
    Text("Profile")
}
