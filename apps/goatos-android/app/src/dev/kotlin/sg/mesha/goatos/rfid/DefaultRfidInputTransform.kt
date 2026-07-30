package sg.mesha.goatos.rfid

import javax.inject.Inject

class DefaultRfidInputTransform @Inject constructor() : RfidInputTransform {
    override fun vaccination(rawRfid: String, shedId: String): String {
        val value = rawRfid.trim()
        return if (value.isNotBlank() && shedId in cptPhoneFixtureShedIds) "CPT-$value" else rawRfid
    }

    private companion object {
        val cptPhoneFixtureShedIds = setOf(
            "91000000-0000-4000-8000-000000000202",
            "92000000-0000-4000-8000-000000000203",
        )
    }
}
