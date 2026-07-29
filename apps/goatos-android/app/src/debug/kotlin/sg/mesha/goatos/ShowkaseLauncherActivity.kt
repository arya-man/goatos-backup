package sg.mesha.goatos

import android.os.Bundle
import androidx.activity.ComponentActivity
import com.airbnb.android.showkase.models.Showkase
import sg.mesha.goatos.ui.getBrowserIntent

class ShowkaseLauncherActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        startActivity(Showkase.getBrowserIntent(this))
        finish()
    }
}
