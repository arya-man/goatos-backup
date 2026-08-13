package sg.mesha.goatos.viewmodel

internal fun proofOverlayContextLine(
    feature: String,
    parkLabel: String? = null,
    locationLabel: String? = null,
    extraLabel: String? = null,
): String =
    listOfNotNull(
        feature.trim().takeIf { it.isNotBlank() },
        parkLabel?.trim()?.takeIf { it.isNotBlank() },
        locationLabel?.trim()?.takeIf { it.isNotBlank() },
        extraLabel?.trim()?.takeIf { it.isNotBlank() },
    ).joinToString(" . ")
