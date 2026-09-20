package sg.mesha.goatos.update

import org.junit.Assert.assertThrows
import org.junit.Test
import java.util.Random
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream
import kotlin.io.path.createTempFile

class SideloadUpdateInstallerTest {

    @Test
    fun `validator rejects html error body saved as apk`() {
        val file = createTempFile(suffix = ".apk").toFile()
        file.writeText("<html>not an apk</html>")

        assertThrows(InvalidApkDownloadException::class.java) {
            SideloadUpdateInstaller.validateDownloadedApk(file)
        }
    }

    @Test
    fun `validator rejects incomplete download when content length differs`() {
        val file = createTempFile(suffix = ".apk").toFile()
        file.writeBytes(ByteArray(2 * 1024 * 1024) { 1 })

        assertThrows(InvalidApkDownloadException::class.java) {
            SideloadUpdateInstaller.validateDownloadedApk(file, expectedBytes = file.length() + 1)
        }
    }

    @Test
    fun `validator accepts apk shaped zip`() {
        val file = createTempFile(suffix = ".apk").toFile()
        val random = Random(42)
        ZipOutputStream(file.outputStream()).use { zip ->
            zip.putNextEntry(ZipEntry("AndroidManifest.xml"))
            zip.write(ByteArray(1024).also { random.nextBytes(it) })
            zip.closeEntry()
            zip.putNextEntry(ZipEntry("classes.dex"))
            zip.write(ByteArray(2 * 1024 * 1024).also { random.nextBytes(it) })
            zip.closeEntry()
        }

        SideloadUpdateInstaller.validateDownloadedApk(file, expectedBytes = file.length())
    }
}
