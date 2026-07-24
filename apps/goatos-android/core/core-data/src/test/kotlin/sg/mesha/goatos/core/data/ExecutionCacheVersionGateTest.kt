package sg.mesha.goatos.core.data

import androidx.room.Room
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import sg.mesha.goatos.core.data.cache.CacheVersionStore
import sg.mesha.goatos.core.data.cache.ExecutionCacheVersionGate
import sg.mesha.goatos.core.data.cache.ExecutionRowsCacheEntity
import sg.mesha.goatos.core.data.cache.ExecutionShedCacheEntity

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class ExecutionCacheVersionGateTest {
    private lateinit var database: GoatDatabase

    private class FakeVersionStore(var version: Int = 0) : CacheVersionStore {
        override suspend fun lastExecutionCacheVersion(): Int = version
        override suspend fun setExecutionCacheVersion(version: Int) {
            this.version = version
        }
    }

    @Before
    fun setUp() {
        database = Room.inMemoryDatabaseBuilder(
            ApplicationProvider.getApplicationContext(),
            GoatDatabase::class.java,
        ).allowMainThreadQueries().build()
    }

    @After
    fun tearDown() {
        database.close()
    }

    private suspend fun seedCaches() {
        database.executionRowsCacheDao().upsert(ExecutionRowsCacheEntity("rows-key", "{}", 1L))
        database.executionShedCacheDao().upsert(ExecutionShedCacheEntity("shed-key", "{}", 1L))
    }

    @Test
    fun `version bump wipes execution read caches and records the new version`() = runTest {
        val rows = database.executionRowsCacheDao()
        val shed = database.executionShedCacheDao()
        val store = FakeVersionStore(version = 6)
        val gate = ExecutionCacheVersionGate(rows, shed, store)
        seedCaches()

        gate.purgeIfVersionChanged(currentVersion = 7)

        assertEquals(0, rows.count())
        assertEquals(0, shed.count())
        assertEquals(7, store.version)
    }

    @Test
    fun `same version leaves the caches untouched`() = runTest {
        val rows = database.executionRowsCacheDao()
        val shed = database.executionShedCacheDao()
        val store = FakeVersionStore(version = 7)
        val gate = ExecutionCacheVersionGate(rows, shed, store)
        seedCaches()

        gate.purgeIfVersionChanged(currentVersion = 7)

        assertEquals(1, rows.count())
        assertEquals(1, shed.count())
    }
}
