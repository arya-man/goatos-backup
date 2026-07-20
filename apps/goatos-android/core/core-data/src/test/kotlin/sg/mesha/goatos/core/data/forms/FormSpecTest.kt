package sg.mesha.goatos.core.data.forms

import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.add
import kotlinx.serialization.json.addJsonObject
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonArray
import kotlinx.serialization.json.putJsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class FormSpecTest {

    /** Mirrors the backend's `goatos.sop-form.v1` fixture (sop service_test.go). */
    private fun vaccinationFormDsl(): Map<String, JsonElement> = buildJsonObject {
        put("schema_version", "goatos.sop-form.v1")
        putJsonArray("fields") {
            addJsonObject {
                put("key", "vaccine_lot_id"); put("label", "Vaccine lot"); put("type", "vaccine_batch_picker"); put("required", true)
            }
            addJsonObject {
                put("key", "cold_chain_verified"); put("label", "Cold chain"); put("type", "boolean"); put("required", true)
            }
            addJsonObject {
                put("key", "goat_ids"); put("label", "Goats"); put("type", "goat_scan"); put("required", true); put("repeat", true)
            }
            addJsonObject {
                put("key", "dose_ml_given"); put("label", "Dose"); put("type", "number"); put("required", true)
            }
            addJsonObject {
                put("key", "administration_video"); put("label", "Video"); put("type", "video_proof"); put("required", true)
            }
        }
        putJsonArray("rules") {
            addJsonObject {
                put("type", "block_submission_if")
                putJsonObject("when") { put("field", "cold_chain_verified"); put("operator", "equals"); put("value", false) }
                put("message", "cold chain must be verified")
            }
            addJsonObject {
                put("type", "proof_required_if")
                put("field", "administration_video")
                putJsonObject("when") { put("field", "goat_ids"); put("operator", "not_empty") }
            }
        }
    }.entries.associate { it.key to it.value }

    @Test
    fun `parses fields with types, required and repeat`() {
        val spec = vaccinationFormDsl().toFormSpec()

        assertEquals("goatos.sop-form.v1", spec.schemaVersion)
        assertEquals(5, spec.fields.size)
        assertEquals(
            listOf(
                FormFieldType.VACCINE_BATCH_PICKER,
                FormFieldType.BOOLEAN,
                FormFieldType.GOAT_SCAN,
                FormFieldType.NUMBER,
                FormFieldType.VIDEO_PROOF,
            ),
            spec.fields.map { it.type },
        )
        val goats = spec.fields.first { it.key == "goat_ids" }
        assertTrue("goat_scan is required", goats.required)
        assertTrue("goat_scan repeats", goats.repeat)
        assertEquals("Goats", goats.label)
        assertFalse("single-value dose does not repeat", spec.fields.first { it.key == "dose_ml_given" }.repeat)
    }

    @Test
    fun `parses conditional rules with when-conditions`() {
        val spec = vaccinationFormDsl().toFormSpec()

        assertEquals(2, spec.rules.size)
        val block = spec.rules.first { it.type == FormRuleType.BLOCK_SUBMISSION_IF }
        assertEquals("cold_chain_verified", block.conditionField)
        assertEquals("equals", block.operator)
        assertEquals("cold chain must be verified", block.message)

        val proof = spec.rules.first { it.type == FormRuleType.PROOF_REQUIRED_IF }
        assertEquals("administration_video", proof.field)
        assertEquals("goat_ids", proof.conditionField)
        assertEquals("not_empty", proof.operator)
    }

    @Test
    fun `unknown field type degrades to UNKNOWN, never throws`() {
        val dsl = buildJsonObject {
            put("schema_version", "goatos.sop-form.v1")
            putJsonArray("fields") {
                addJsonObject { put("key", "future_widget"); put("label", "New"); put("type", "holographic_scanner") }
            }
        }.entries.associate { it.key to it.value }

        val spec = dsl.toFormSpec()
        assertEquals(FormFieldType.UNKNOWN, spec.fields.single().type)
    }

    @Test
    fun `select date time description and string options use supported renderers`() {
        val dsl = buildJsonObject {
            putJsonArray("fields") {
                addJsonObject {
                    put("key", "route_site")
                    put("label", "Route / site")
                    put("type", "select")
                    put("description", "Route and body site used for administration.")
                    putJsonArray("options") { add("subcutaneous"); add("intramuscular") }
                }
                addJsonObject {
                    put("key", "administered_at")
                    put("label", "Administered at")
                    put("type", "date_time")
                }
            }
        }.entries.associate { it.key to it.value }

        val fields = dsl.toFormSpec().fields
        assertEquals(FormFieldType.SELECT, fields[0].type)
        assertEquals(listOf("subcutaneous", "intramuscular"), fields[0].options.map { it.value })
        assertEquals("Route and body site used for administration.", fields[0].helpText)
        assertEquals(FormFieldType.DATE_TIME, fields[1].type)
    }

    @Test
    fun `empty or malformed dsl yields the empty spec`() {
        assertTrue(emptyMap<String, JsonElement>().toFormSpec().isEmpty)
        // a field with no key is dropped, not crashed
        val noKey = buildJsonObject {
            putJsonArray("fields") { addJsonObject { put("label", "orphan") } }
        }.entries.associate { it.key to it.value }
        assertTrue(noKey.toFormSpec().fields.isEmpty())
    }

    @Test
    fun `inline picker options are parsed`() {
        val dsl = buildJsonObject {
            putJsonArray("fields") {
                addJsonObject {
                    put("key", "site"); put("label", "Site"); put("type", "location_picker")
                    putJsonArray("options") {
                        addJsonObject { put("value", "cbe"); put("label", "Coimbatore") }
                        addJsonObject { put("value", "cpt"); put("label", "Channapatna") }
                    }
                }
            }
        }.entries.associate { it.key to it.value }

        val field = dsl.toFormSpec().fields.single()
        assertEquals(listOf("cbe", "cpt"), field.options.map { it.value })
        assertEquals("Coimbatore", field.options.first().label)
        assertNull(field.helpText)
    }

    @Test
    fun `picker option source is parsed for backend task resolution`() {
        val dsl = buildJsonObject {
            putJsonArray("fields") {
                addJsonObject {
                    put("key", "vaccine_lot_id")
                    put("label", "Vaccine lot")
                    put("type", "vaccine_batch_picker")
                    put("option_source", "vaccine_lots")
                }
            }
        }.entries.associate { it.key to it.value }

        assertEquals("vaccine_lots", dsl.toFormSpec().fields.single().optionSource)
    }
}
