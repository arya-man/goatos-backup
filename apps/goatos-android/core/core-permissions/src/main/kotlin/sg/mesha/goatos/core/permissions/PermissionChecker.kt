package sg.mesha.goatos.core.permissions

import android.app.Activity
import android.content.Context
import android.content.pm.PackageManager
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat

/**
 * Thin `ContextCompat`/`ActivityCompat` wrappers kept here (not in feature code) so any
 * feature can query current grant state / rationale-eligibility without re-importing
 * `androidx.core.*` at every call site.
 */
fun isPermissionGranted(context: Context, permission: AppPermission): Boolean =
    ContextCompat.checkSelfPermission(context, permission.manifestPermission) ==
        PackageManager.PERMISSION_GRANTED

fun shouldShowRationale(activity: Activity, permission: AppPermission): Boolean =
    ActivityCompat.shouldShowRequestPermissionRationale(activity, permission.manifestPermission)
