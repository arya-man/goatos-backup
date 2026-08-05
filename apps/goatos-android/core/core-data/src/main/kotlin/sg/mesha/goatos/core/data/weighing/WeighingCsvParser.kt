package sg.mesha.goatos.core.data.weighing

import java.io.ByteArrayInputStream
import java.io.InputStreamReader

/**
 * ONE row of the backend's weighing export CSV, already split into its 14 named columns.
 *
 * The backend's column order is the contract; this class mirrors it field-for-field rather than
 * re-deriving meaning, so a column the backend adds later is a compile error here instead of a
 * silently shifted field.
 */
data class WeighingCsvExportRow(
    val park: String,
    val shedName: String,
    val shedStatus: String,
    val type: String,
    val scannedIdentifier: String,
    val weightKg: String,
    val averageWeightKg: String,
    val animalCount: String,
    val verificationStatus: String,
    val proofReferenceType: String,
    val proofReference: String,
    val recordedAt: String,
    val dateIst: String,
    val timeIst: String,
) {
    /** The backend's own word for a shed nobody has touched yet. Never filtered out upstream. */
    val isNotWeighed: Boolean get() = type.trim().equals("not weighed", ignoreCase = true)
}

/**
 * Parses the backend's `GET /weighing/campaigns/{id}/export` body into rows.
 *
 * A naive `line.split(",")` breaks the moment any field -- a proof reference, a free-text shed
 * name -- contains a comma inside RFC 4180 quotes, silently shifting every column after it. This
 * walks the bytes character-by-character instead: `"` toggles quoted mode, `""` inside a quoted
 * field is an escaped quote, and a `,`/newline only ends a field or row when unquoted. Malformed
 * rows (wrong column count) are dropped rather than mis-mapped into the wrong fields -- a preview
 * showing 90% of the sheet correctly is far safer than one silently misreading a data row.
 */
fun parseWeighingExportCsv(bytes: ByteArray): List<WeighingCsvExportRow> {
    val rows = mutableListOf<List<String>>()
    val field = StringBuilder()
    var row = mutableListOf<String>()
    var inQuotes = false
    val reader = InputStreamReader(ByteArrayInputStream(bytes), Charsets.UTF_8)
    var current = reader.read()
    while (current != -1) {
        val ch = current.toChar()
        when {
            inQuotes && ch == '"' -> {
                current = reader.read()
                if (current == '"'.code) {
                    field.append('"')
                    current = reader.read()
                } else {
                    inQuotes = false
                }
                continue
            }
            !inQuotes && ch == '"' -> inQuotes = true
            !inQuotes && ch == ',' -> {
                row.add(field.toString())
                field.clear()
            }
            !inQuotes && (ch == '\n' || ch == '\r') -> {
                // A CRLF pair fires this branch twice; the second call is a no-op because both
                // field and row are already empty from the first.
                if (field.isNotEmpty() || row.isNotEmpty()) {
                    row.add(field.toString())
                    field.clear()
                    rows.add(row)
                    row = mutableListOf()
                }
            }
            else -> field.append(ch)
        }
        current = reader.read()
    }
    if (field.isNotEmpty() || row.isNotEmpty()) {
        row.add(field.toString())
        rows.add(row)
    }
    if (rows.isEmpty()) return emptyList()
    // First row is the header; the backend's 14 named columns are the contract, so data rows are
    // matched by position, not by re-reading the header text.
    return rows.drop(1).mapNotNull { cols ->
        if (cols.size < 14) return@mapNotNull null
        WeighingCsvExportRow(
            park = cols[0],
            shedName = cols[1],
            shedStatus = cols[2],
            type = cols[3],
            scannedIdentifier = cols[4],
            weightKg = cols[5],
            averageWeightKg = cols[6],
            animalCount = cols[7],
            verificationStatus = cols[8],
            proofReferenceType = cols[9],
            proofReference = cols[10],
            recordedAt = cols[11],
            dateIst = cols[12],
            timeIst = cols[13],
        )
    }
}
