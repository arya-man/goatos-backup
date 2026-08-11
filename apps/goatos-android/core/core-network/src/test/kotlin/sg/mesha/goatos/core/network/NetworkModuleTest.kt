package sg.mesha.goatos.core.network

import okhttp3.OkHttpClient
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertEquals
import org.junit.Test

class NetworkModuleTest {

    @Test
    fun bearerAuthInterceptorAddsTenantAndLocaleHeaders() {
        var captured: Request? = null
        val client = OkHttpClient.Builder()
            .addInterceptor(
                BearerAuthInterceptor(
                    tokenProvider = { "token-1" },
                    tenantIdProvider = { "tenant-1" },
                    localeProvider = { "hi" },
                ),
            )
            .addInterceptor { chain ->
                captured = chain.request()
                Response.Builder()
                    .request(chain.request())
                    .protocol(Protocol.HTTP_1_1)
                    .code(200)
                    .message("OK")
                    .body("{}".toResponseBody())
                    .build()
            }
            .build()

        client.newCall(Request.Builder().url("http://localhost/app/bootstrap").build()).execute().close()

        val request = requireNotNull(captured)
        assertEquals("Bearer token-1", request.header("Authorization"))
        assertEquals("tenant-1", request.header(TENANT_CONTEXT_HEADER))
        assertEquals("hi, en;q=0.8", request.header(ACCEPT_LANGUAGE_HEADER))
        assertEquals("hi", request.header(LOCALE_CONTEXT_HEADER))
    }

    @Test
    fun bearerAuthInterceptorAddsClientMetadataHeaders() {
        var captured: Request? = null
        val client = OkHttpClient.Builder()
            .addInterceptor(
                BearerAuthInterceptor(
                    tokenProvider = { null },
                    requestMetadataProvider = {
                        RequestMetadata(
                            appVersion = "0.1.17",
                            appVersionCode = "18",
                            buildType = "stgRelease",
                            deviceId = "install-123",
                            platform = "android",
                            osVersion = "Android 14",
                            sdkVersion = "34",
                            deviceModel = "Infinix X",
                        )
                    },
                ),
            )
            .addInterceptor { chain ->
                captured = chain.request()
                Response.Builder()
                    .request(chain.request())
                    .protocol(Protocol.HTTP_1_1)
                    .code(200)
                    .message("OK")
                    .body("{}".toResponseBody())
                    .build()
            }
            .build()

        client.newCall(Request.Builder().url("http://localhost/app/bootstrap").build()).execute().close()

        val request = requireNotNull(captured)
        assertEquals("0.1.17", request.header("X-GoatOS-App-Version"))
        assertEquals("18", request.header("X-GoatOS-App-Version-Code"))
        assertEquals("stgRelease", request.header("X-GoatOS-Build-Type"))
        assertEquals("install-123", request.header("X-GoatOS-Device-Id"))
        assertEquals("install-123", request.header("X-Device-Id"))
        assertEquals("android", request.header("X-GoatOS-Platform"))
        assertEquals("Android 14", request.header("X-GoatOS-OS-Version"))
        assertEquals("34", request.header("X-GoatOS-SDK-Version"))
        assertEquals("Infinix X", request.header("X-GoatOS-Device-Model"))
    }

    @Test
    fun bearerAuthInterceptorFallsBackToEnglishForUnsafeLocale() {
        var captured: Request? = null
        val client = OkHttpClient.Builder()
            .addInterceptor(BearerAuthInterceptor(tokenProvider = { null }, localeProvider = { "hi\nx" }))
            .addInterceptor { chain ->
                captured = chain.request()
                Response.Builder()
                    .request(chain.request())
                    .protocol(Protocol.HTTP_1_1)
                    .code(200)
                    .message("OK")
                    .body("{}".toResponseBody())
                    .build()
            }
            .build()

        client.newCall(Request.Builder().url("http://localhost/app/config").build()).execute().close()

        val request = requireNotNull(captured)
        assertEquals("en", request.header(ACCEPT_LANGUAGE_HEADER))
        assertEquals("en", request.header(LOCALE_CONTEXT_HEADER))
    }
}
