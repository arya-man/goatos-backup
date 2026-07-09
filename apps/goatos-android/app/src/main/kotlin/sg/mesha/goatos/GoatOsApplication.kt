package sg.mesha.goatos

import android.app.Application

/**
 * Application entry point. Kept thin (TRD §3): the DI graph, boot, and nav host
 * live here as the skeleton grows. Hilt is added in the core-data / DI phase.
 */
class GoatOsApplication : Application()
