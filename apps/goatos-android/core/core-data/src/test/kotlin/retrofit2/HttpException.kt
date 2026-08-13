package retrofit2

class HttpException(private val statusCode: Int) : RuntimeException("HTTP $statusCode") {
    fun code(): Int = statusCode
}
