package sg.mesha.goatos.core.data.forms

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import sg.mesha.goatos.core.network.dto.SopVersionDto

/**
 * Typed, render-ready view of a SOP `form_dsl` (`schema_version` `goatos.sop-form.v1`). The
 * backend owns the schema (golden frontend rule); this is a STRUCTURAL walk of the free-form
 * JSON into fields + conditional rules the operator form runner draws — it never invents fields.
 * Unknown field types survive as [FormFieldType.UNKNOWN] so a newer backend field renders as a
 * safe read-only placeholder rather than crashing an older app.
 */
enum class FormFieldType {
    BOOLEAN, NUMBER, TEXT, GOAT_SCAN, VACCINE_BATCH_PICKER, LOCATION_PICKER, VIDEO_PROOF, UNKNOWN;

    companion object {
        /** Maps the backend field `type` (incl. known aliases) to a renderer type. */
        fun from(raw: String): FormFieldType = when (raw.trim().lowercase()) {
            "boolean", "checkbox", "toggle" -> BOOLEAN
            "number", "integer", "decimal" -> NUMBER
            "text", "string", "note", "textarea" -> TEXT
            "goat_scan", "goat_lookup", "animal_id_scan", "rfid_scan" -> GOAT_SCAN
            "vaccine_batch_picker", "vaccine_lot_picker", "batch_picker" -> VACCINE_BATCH_PICKER
            "location_picker", "shed_picker", "park_picker" -> LOCATION_PICKER
            "video_proof", "photo_proof", "media_proof" -> VIDEO_PROOF
            else -> UNKNOWN
        }
    }
}

enum class FormRuleType {
    VISIBLE_IF, REQUIRED_IF, ENABLED_IF, PROOF_REQUIRED_IF, BLOCK_SUBMISSION_IF, REQUIRES_SUPERVISOR_IF, UNKNOWN;

    companion object {
        fun from(raw: String): FormRuleType = when (raw.trim().lowercase()) {
            "visible_if" -> VISIBLE_IF
            "required_if" -> REQUIRED_IF
            "enabled_if" -> ENABLED_IF
            "proof_required_if" -> PROOF_REQUIRED_IF
            "block_submission_if" -> BLOCK_SUBMISSION_IF
            "requires_supervisor_if" -> REQUIRES_SUPERVISOR_IF
            else -> UNKNOWN
        }
    }
}

data class FormOption(val value: String, val label: String)

data class FormField(
    val key: String,
    val label: String,
    val type: FormFieldType,
    val required: Boolean = false,
    /** true = the operator can add multiple values (e.g. scan many goats). */
    val repeat: Boolean = false,
    val helpText: String? = null,
    /** Inline options for pickers when the backend embeds them; otherwise fetched by key. */
    val options: List<FormOption> = emptyList(),
)

/** A conditional rule (`when` a [conditionField] [operator]s [value], apply the rule to [field]). */
data class FormRule(
    val type: FormRuleType,
    val field: String? = null,
    val conditionField: String? = null,
    val operator: String? = null,
    val value: JsonElement? = null,
    val message: String? = null,
)

data class FormSpec(
    val schemaVersion: String,
    val fields: List<FormField>,
    val rules: List<FormRule>,
) {
    val isEmpty: Boolean get() = fields.isEmpty()

    companion object {
        val Empty = FormSpec(schemaVersion = "", fields = emptyList(), rules = emptyList())
    }
}

/** Parses this SOP version's `form_dsl` into a [FormSpec]; malformed/absent DSL → [FormSpec.Empty]. */
fun SopVersionDto.toFormSpec(): FormSpec = formDsl.toFormSpec()

/** Parses a raw `form_dsl` map into a [FormSpec]. Null-safe: bad shapes drop to defaults, never throw. */
fun Map<String, JsonElement>.toFormSpec(): FormSpec {
    if (isEmpty()) return FormSpec.Empty
    val fields = (this["fields"] as? JsonArray).orEmpty().mapNotNull { it.toFormField() }
    val rules = (this["rules"] as? JsonArray).orEmpty().mapNotNull { it.toFormRule() }
    return FormSpec(
        schemaVersion = this["schema_version"].asStringOrEmpty(),
        fields = fields,
        rules = rules,
    )
}

private fun JsonArray?.orEmpty(): List<JsonElement> = this ?: emptyList()

private fun JsonElement.toFormField(): FormField? {
    val obj = this as? JsonObject ?: return null
    val key = obj["key"].asStringOrEmpty().ifBlank { return null }
    return FormField(
        key = key,
        label = obj["label"].asStringOrEmpty().ifBlank { key },
        type = FormFieldType.from(obj["type"].asStringOrEmpty()),
        required = obj["required"].asBool(),
        repeat = obj["repeat"].asBool(),
        helpText = obj["help_text"].asStringOrNull(),
        options = (obj["options"] as? JsonArray).orEmpty().mapNotNull { it.toFormOption() },
    )
}

private fun JsonElement.toFormOption(): FormOption? {
    val obj = this as? JsonObject ?: return null
    val value = obj["value"].asStringOrEmpty().ifBlank { return null }
    return FormOption(value = value, label = obj["label"].asStringOrEmpty().ifBlank { value })
}

private fun JsonElement.toFormRule(): FormRule? {
    val obj = this as? JsonObject ?: return null
    val type = FormRuleType.from(obj["type"].asStringOrEmpty())
    val whenObj = obj["when"] as? JsonObject
    return FormRule(
        type = type,
        field = obj["field"].asStringOrNull(),
        conditionField = whenObj?.get("field").asStringOrNull(),
        operator = whenObj?.get("operator").asStringOrNull(),
        value = whenObj?.get("value"),
        message = obj["message"].asStringOrNull(),
    )
}

private fun JsonElement?.asStringOrEmpty(): String =
    (this as? JsonPrimitive)?.takeIf { it.isString }?.content ?: ""

private fun JsonElement?.asStringOrNull(): String? =
    (this as? JsonPrimitive)?.takeIf { it.isString }?.content

private fun JsonElement?.asBool(): Boolean =
    (this as? JsonPrimitive)?.booleanOrNull ?: false
