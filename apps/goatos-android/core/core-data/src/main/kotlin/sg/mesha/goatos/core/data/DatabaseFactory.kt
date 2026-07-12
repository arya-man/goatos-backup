package sg.mesha.goatos.core.data

import android.content.Context
import androidx.room.Room

/** Builds the app database. Callers (DI) supply the application context. Never destructive —
 *  [MIGRATION_1_2] carries the on-device cache forward across the bootstrap-only v1 schema. */
fun buildGoatDatabase(context: Context): GoatDatabase =
    Room.databaseBuilder(context, GoatDatabase::class.java, "goatos.db")
        .addMigrations(MIGRATION_1_2, MIGRATION_2_3, MIGRATION_3_4, MIGRATION_4_5)
        .build()
