package sg.mesha.goatos

import android.app.Application
import dagger.hilt.android.HiltAndroidApp

/** Application entry point + Hilt DI root. Kept thin (TRD §3). */
@HiltAndroidApp
class GoatOsApplication : Application()
