package sg.mesha.goatos.rfid

/** Adjusts raw hardware-reader input before feature-specific matching and sync. */
interface RfidInputTransform {
    fun vaccination(rawRfid: String, shedId: String): String
}

object PassthroughRfidInputTransform : RfidInputTransform {
    override fun vaccination(rawRfid: String, shedId: String): String = rawRfid
}
