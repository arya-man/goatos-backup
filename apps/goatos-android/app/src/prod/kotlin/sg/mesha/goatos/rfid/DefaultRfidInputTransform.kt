package sg.mesha.goatos.rfid

import javax.inject.Inject

class DefaultRfidInputTransform @Inject constructor() : RfidInputTransform {
    override fun vaccination(rawRfid: String, shedId: String): String = rawRfid
}
