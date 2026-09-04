package sg.mesha.goatos.update

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.core.content.FileProvider
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File
import java.net.URL
import javax.inject.Inject
import javax.inject.Singleton

sealed interface SideloadInstallResult {
    data object InstallerOpened : SideloadInstallResult
    data object InstallPermissionNeeded : SideloadInstallResult
}

@Singleton
class SideloadUpdateInstaller @Inject constructor(
    @ApplicationContext private val context: Context,
) {
    fun canInstallFromThisSource(): Boolean =
        Build.VERSION.SDK_INT < Build.VERSION_CODES.O || context.packageManager.canRequestPackageInstalls()

    suspend fun downloadAndOpenInstaller(apkUrl: String): SideloadInstallResult {
        if (!canInstallFromThisSource()) {
            openUnknownAppSourcesSettings()
            return SideloadInstallResult.InstallPermissionNeeded
        }

        val apkFile = downloadApk(apkUrl)
        openInstaller(apkFile)
        return SideloadInstallResult.InstallerOpened
    }

    suspend fun downloadApk(apkUrl: String): File = withContext(Dispatchers.IO) {
        val updatesDir = File(context.cacheDir, "updates").apply { mkdirs() }
        val apkFile = File(updatesDir, "goatos-update.apk")
        val connection = URL(apkUrl).openConnection().apply {
            connectTimeout = DOWNLOAD_CONNECT_TIMEOUT_MS
            readTimeout = DOWNLOAD_READ_TIMEOUT_MS
        }
        connection.getInputStream().use { input ->
            apkFile.outputStream().use { output -> input.copyTo(output) }
        }
        apkFile
    }

    fun openUnknownAppSourcesSettings() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val intent = Intent(
            Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
            Uri.parse("package:${context.packageName}"),
        ).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        context.startActivity(intent)
    }

    fun openInstaller(apkFile: File): SideloadInstallResult {
        if (!canInstallFromThisSource()) {
            openUnknownAppSourcesSettings()
            return SideloadInstallResult.InstallPermissionNeeded
        }
        val apkUri = FileProvider.getUriForFile(
            context,
            "${context.packageName}.fileprovider",
            apkFile,
        )
        val intent = Intent(Intent.ACTION_VIEW).apply {
            setDataAndType(apkUri, APK_MIME_TYPE)
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
        }
        try {
            context.startActivity(intent)
        } catch (e: ActivityNotFoundException) {
            throw IllegalStateException("No Android package installer is available.", e)
        }
        return SideloadInstallResult.InstallerOpened
    }

    companion object {
        private const val APK_MIME_TYPE = "application/vnd.android.package-archive"
        private const val DOWNLOAD_CONNECT_TIMEOUT_MS = 15_000
        private const val DOWNLOAD_READ_TIMEOUT_MS = 60_000
    }
}
