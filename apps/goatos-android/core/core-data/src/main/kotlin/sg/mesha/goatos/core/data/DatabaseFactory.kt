package sg.mesha.goatos.core.data

import android.content.Context
import androidx.room.Room

/** Builds the app database. Callers (DI) supply the application context. */
fun buildGoatDatabase(context: Context): GoatDatabase =
    Room.databaseBuilder(context, GoatDatabase::class.java, "goatos.db").build()
