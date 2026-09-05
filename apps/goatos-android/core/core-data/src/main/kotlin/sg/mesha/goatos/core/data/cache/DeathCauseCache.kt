package sg.mesha.goatos.core.data.cache

import androidx.room.Dao
import androidx.room.Entity
import androidx.room.Insert
import androidx.room.OnConflictStrategy
import androidx.room.PrimaryKey
import androidx.room.Query
import kotlinx.coroutines.flow.Flow

/**
 * The death form's disease vocabulary, cached on the device.
 *
 * OFFLINE-FIRST IS THE WHOLE POINT HERE, not a convention being satisfied. An operator recording a
 * death is standing in a pen, often with no signal, and the moment they need the disease list is
 * the moment they can least reach the server. Without this row the "due to disease" choice would
 * be unavailable exactly when it is used, and every such death would be filed as normal — a
 * silent, permanent loss of the fact this whole feature exists to capture.
 *
 * ONE ROW, and there deliberately is no scope key beyond [SCOPE]: the catalog is tenant-wide, has
 * no filters, and is static for the life of a server process (the diagnosis registers are embedded
 * YAML validated at start-up). A refresh REPLACEs the single row, so the table cannot grow and
 * needs no eviction pass of its own.
 *
 * A JSON BLOB rather than a row per disease, which is the opposite of the shape the paged work
 * queues use, and for the opposite reason: this is not a paged list. It is a few dozen entries
 * read whole into a form control and searched in memory, so a row-per-disease table would buy
 * paging nothing and cost a second read path.
 */
@Entity(tableName = "death_cause_catalog")
data class DeathCauseCatalogEntity(
    @PrimaryKey val scopeKey: String,
    val dtoJson: String,
    val updatedAt: Long,
) {
    companion object {
        /** The only key this table ever holds. See the class note. */
        const val SCOPE = "death-causes"
    }
}

@Dao
interface DeathCauseCatalogDao {
    @Query("SELECT * FROM death_cause_catalog WHERE scopeKey = :scopeKey")
    fun observe(scopeKey: String): Flow<DeathCauseCatalogEntity?>

    @Insert(onConflict = OnConflictStrategy.REPLACE)
    suspend fun upsert(entity: DeathCauseCatalogEntity)

    @Query("DELETE FROM death_cause_catalog WHERE scopeKey = :scopeKey")
    suspend fun delete(scopeKey: String)
}
