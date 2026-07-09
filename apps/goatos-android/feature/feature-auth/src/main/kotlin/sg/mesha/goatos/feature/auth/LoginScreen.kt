package sg.mesha.goatos.feature.auth

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp

/**
 * Sign-in. Goat OS sign-in is email-always (backend claims a verified email into a
 * scope grant + provisions the HR department). The OTP / magic-link / Firebase flow
 * is gated; this collects the email and hands it to the caller, which starts the
 * session. Real OTP entry + verification land with the Firebase auth pass.
 */
@Composable
fun LoginScreen(
    onSignIn: (email: String) -> Unit,
    modifier: Modifier = Modifier,
) {
    var email by remember { mutableStateOf("") }

    Column(
        modifier = modifier.fillMaxSize().padding(24.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(text = "Goat OS", style = MaterialTheme.typography.headlineMedium)
        Text(
            text = "Sign in with your work email",
            style = MaterialTheme.typography.bodyMedium,
            modifier = Modifier.padding(top = 4.dp, bottom = 24.dp),
        )
        OutlinedTextField(
            value = email,
            onValueChange = { email = it },
            label = { Text("Email") },
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Email),
            modifier = Modifier.fillMaxWidth(),
        )
        Button(
            onClick = { onSignIn(email.trim()) },
            enabled = email.contains("@"),
            modifier = Modifier.fillMaxWidth().padding(top = 16.dp),
        ) {
            Text("Continue")
        }
    }
}
