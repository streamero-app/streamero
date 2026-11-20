package net.scyde.streamero.manage

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import net.scyde.streamero.api.Api
import net.scyde.streamero.api.json
import okhttp3.FormBody
import okhttp3.HttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody
import okhttp3.RequestBody.Companion.toRequestBody

/**
 * The media-management services the server proxies (D-048). Everything goes through the
 * Streamero server with the user's own session; the server adds the services' API keys.
 * [apiBase] is the default; the server's `/arr/services` answer wins when it names one.
 */
enum class ArrService(val id: String, val title: String, val apiBase: String) {
    Radarr("radarr", "Radarr", "api/v3"),
    Sonarr("sonarr", "Sonarr", "api/v3"),
    Lidarr("lidarr", "Lidarr", "api/v1"),
    Prowlarr("prowlarr", "Prowlarr", "api/v1"),
    Bazarr("bazarr", "Bazarr", "api"),
    Seerr("seerr", "Seerr", "api/v1");

    /** Radarr, Sonarr and Lidarr share the library/queue/wanted shapes. */
    val isLibrary: Boolean get() = this == Radarr || this == Sonarr || this == Lidarr
    /** The *arr family (incl. Prowlarr): same system, provider and config resources. */
    val isArr: Boolean get() = isLibrary || this == Prowlarr

    companion object {
        fun of(id: String?): ArrService? = entries.firstOrNull { it.id == id }
    }
}

@Serializable
data class ArrServiceStatus(
    val id: String,
    val configured: Boolean = false,
    val reachable: Boolean = false,
    val version: String? = null,
    val error: String? = null,
    val apiBase: String? = null,
)

@Serializable
data class ArrServices(val services: List<ArrServiceStatus> = emptyList())

/** One validation failure from an *arr service (`[{propertyName, errorMessage, severity}]`). */
data class ArrValidation(val property: String, val message: String, val warning: Boolean = false)

/**
 * A failed call. [code] is the Streamero server's own error code when the proxy refused the
 * call (`unauthenticated`, `forbidden`, `read_only_key`, `invalid_path`, `method_not_allowed`,
 * `arr_not_configured`, `request_too_large`, `rate_limited`, `arr_unreachable`; D-049, D-052).
 * When the service itself answered, it is `validation` (field errors in [validation]),
 * `arr_auth` (the service refused the stored API key), `request_too_large`, `rate_limited`,
 * `arr_unreachable` (a gateway error without a body) or `upstream`. [detail] is the service's
 * own message, when it sent a readable one.
 */
class ArrException(
    val status: Int,
    val code: String,
    message: String,
    val validation: List<ArrValidation> = emptyList(),
    val detail: String? = null,
) : Exception(message) {
    /** The validation message for a form field (matched on the last segment of the property name). */
    fun messageFor(field: String): String? = validation.firstOrNull { matches(it.property, field) }?.message

    /** Validation failures no field of [fields] claims, to show above the form. */
    fun unmatched(fields: Collection<String>): List<ArrValidation> = validation.filter { v -> fields.none { matches(v.property, it) } }

    companion object {
        fun matches(property: String, field: String): Boolean {
            val last = property.substringAfterLast('.').substringBefore('[')
            return last.equals(field, ignoreCase = true) || property.equals(field, ignoreCase = true)
        }

        /** The codes the Streamero server itself answers with (its `{code, message}` body). */
        val SERVER_CODES = setOf(
            "unauthenticated", "forbidden", "read_only_key", "invalid_path", "method_not_allowed", "arr_not_configured",
            "request_too_large", "rate_limited", "arr_unreachable", "not_found", "generic", "internal", "bad_request",
        )

        /** The code for an answer of the service itself (no Streamero code in the body). */
        private fun upstreamCode(status: Int, hasMessage: Boolean): String = when {
            status == 401 -> "arr_auth"
            status == 413 -> "request_too_large"
            status == 429 -> "rate_limited"
            !hasMessage && (status == 502 || status == 503 || status == 504) -> "arr_unreachable"
            !hasMessage && status == 404 -> "not_found"
            else -> "upstream"
        }

        private fun texts(e: JsonElement?): List<String> = when (e) {
            is JsonArray -> e.flatMap { texts(it) }
            is JsonObject -> listOfNotNull(e.s("errorMessage") ?: e.s("message") ?: e.s("msg"))
            is JsonPrimitive -> listOfNotNull(e.contentOrNull?.takeIf { it.isNotBlank() })
            else -> emptyList()
        }

        /**
         * Builds the error for a non-2xx answer from its status and body: the server's
         * `{code, message}`, the services' `[{propertyName, errorMessage}]`, `{message}`,
         * `{error}`, `{title, errors: {field: [..]}}`, a JSON string, or anything else (HTML, an
         * empty body), which never shows up as markup.
         */
        fun parse(status: Int, body: String): ArrException {
            val trimmed = body.trim()
            val el = if (trimmed.startsWith("{") || trimmed.startsWith("[") || trimmed.startsWith("\"")) {
                try { json.parseToJsonElement(trimmed) } catch (e: Exception) { null }
            } else null
            if (el is JsonArray) {
                val v = el.mapNotNull { e ->
                    val o = e as? JsonObject ?: return@mapNotNull null
                    val msg = o.s("errorMessage") ?: o.s("message") ?: return@mapNotNull null
                    ArrValidation(o.s("propertyName").orEmpty(), msg, o.s("severity").equals("warning", true) || o.b("isWarning") == true)
                }
                if (v.isNotEmpty()) return ArrException(status, "validation", v.joinToString("\n") { it.message }, v, v.joinToString("\n") { it.message })
                val t = texts(el)
                if (t.isNotEmpty()) return upstream(status, t.joinToString("\n"))
            }
            if (el is JsonObject) {
                val code = el.s("code")
                val message = (el.s("message") ?: el.s("error") ?: el.s("errorMessage") ?: el.o("error")?.s("message") ?: el.s("title"))?.takeIf { it.isNotBlank() }
                // The Streamero server's own errors carry a code; the services' errors only a message.
                if (code != null && code in SERVER_CODES) return ArrException(status, code, message ?: code)
                // ASP.NET style: {"title": "...", "errors": {"Path": ["..."]}}
                val fieldErrors = el.o("errors")?.flatMap { (k, v) -> texts(v).map { ArrValidation(k, it) } }.orEmpty()
                if (fieldErrors.isNotEmpty()) return ArrException(status, "validation", fieldErrors.joinToString("\n") { it.message }, fieldErrors, fieldErrors.joinToString("\n") { it.message })
                val listed = texts(el.a("errors"))
                if (message != null) {
                    val detail = el.s("description")?.takeIf { it.isNotBlank() && it != message }
                    val full = listOfNotNull(if (detail != null) "$message — $detail" else message, listed.joinToString("; ").ifBlank { null }).joinToString("\n")
                    return upstream(status, full)
                }
                if (listed.isNotEmpty()) return upstream(status, listed.joinToString("\n"))
            }
            if (el is JsonPrimitive) el.contentOrNull?.takeIf { it.isNotBlank() }?.let { return upstream(status, it.take(300)) }
            // Plain text is shown (short); HTML and empty bodies are not.
            val plain = trimmed.takeIf { el == null && it.isNotEmpty() && !it.startsWith("<") && !it.startsWith("{") && !it.startsWith("[") }?.take(300)
            if (plain != null) return upstream(status, plain)
            return ArrException(status, upstreamCode(status, false), "HTTP $status")
        }

        private fun upstream(status: Int, message: String) = ArrException(status, upstreamCode(status, true), message, detail = message)
    }
}

/**
 * Calls one service through the server's proxy:
 * `/api/v1/arr/{service}/proxy/{apiBase}/{resource}?{query}`.
 */
class ArrClient(val api: Api, val service: ArrService, apiBase: String? = null) {
    val apiBase: String = (apiBase ?: service.apiBase).trim('/')

    /** The proxy URL for a resource of the service's API (`movie/12`, `config/host`). */
    fun url(resource: String, query: Map<String, Any?> = emptyMap()): HttpUrl {
        val b = api.base.newBuilder()
        (listOf("api", "v1", "arr", service.id, "proxy") + apiBase.split('/') + resource.trimStart('/').substringBefore('?').split('/'))
            // "." and ".." would walk out of the proxy prefix: dropped (the server checks too).
            .forEach { if (it.isNotEmpty() && it != "." && it != "..") b.addPathSegment(it) }
        resource.substringAfter('?', "").split('&').filter { it.isNotEmpty() }.forEach {
            b.addQueryParameter(it.substringBefore('='), it.substringAfter('=', ""))
        }
        query.forEach { (k, v) ->
            when (v) {
                null -> {}
                is Collection<*> -> v.forEach { x -> if (x != null) b.addQueryParameter(k, x.toString()) }
                else -> b.addQueryParameter(k, v.toString())
            }
        }
        return b.build()
    }

    /** An image of the service (Radarr/Sonarr/Lidarr `images[].url`, `/MediaCover/...`); external URLs as they are. */
    fun imageUrl(url: String?): String? = imageUrl(api, service, url)

    suspend fun get(resource: String, query: Map<String, Any?> = emptyMap()): JsonElement =
        request("GET", resource, null, query) ?: JsonNull

    suspend fun obj(resource: String, query: Map<String, Any?> = emptyMap()): JsonObject = get(resource, query) as? JsonObject ?: JsonObject(emptyMap())

    suspend fun list(resource: String, query: Map<String, Any?> = emptyMap()): List<JsonObject> = when (val e = get(resource, query)) {
        is JsonArray -> e.filterIsInstance<JsonObject>()
        // Paged answers: *arr `records`, Seerr `results`, Bazarr `data`.
        is JsonObject -> (e["records"] ?: e["results"] ?: e["data"]).let { (it as? JsonArray)?.filterIsInstance<JsonObject>() ?: emptyList() }
        else -> emptyList()
    }

    suspend fun post(resource: String, body: JsonElement? = null, query: Map<String, Any?> = emptyMap()): JsonElement? = request("POST", resource, body, query)
    suspend fun put(resource: String, body: JsonElement, query: Map<String, Any?> = emptyMap()): JsonElement? = request("PUT", resource, body, query)
    suspend fun patch(resource: String, body: JsonElement? = null, query: Map<String, Any?> = emptyMap()): JsonElement? = request("PATCH", resource, body, query)
    suspend fun delete(resource: String, query: Map<String, Any?> = emptyMap()): JsonElement? = request("DELETE", resource, null, query)

    /** Bazarr takes some of its writes as form fields instead of JSON. */
    suspend fun postForm(resource: String, fields: List<Pair<String, String>>, query: Map<String, Any?> = emptyMap(), method: String = "POST"): JsonElement? {
        val b = FormBody.Builder()
        fields.forEach { (k, v) -> b.add(k, v) }
        return raw(method, resource, b.build(), query)
    }

    /** Runs an *arr command (`POST command {name, ...}`), e.g. `MoviesSearch` with `movieIds`. */
    suspend fun command(name: String, vararg args: Pair<String, Any?>): JsonElement? =
        post("command", jsonOf("name" to name, *args))

    suspend fun request(method: String, resource: String, body: JsonElement?, query: Map<String, Any?>): JsonElement? {
        val rb = when {
            body != null -> json.encodeToString(JsonElement.serializer(), body).toRequestBody(JSON)
            method == "GET" || method == "DELETE" -> null
            else -> ByteArray(0).toRequestBody(null)
        }
        return raw(method, resource, rb, query)
    }

    suspend fun raw(method: String, resource: String, body: RequestBody?, query: Map<String, Any?>): JsonElement? = withContext(Dispatchers.IO) {
        val req = Request.Builder().url(url(resource, query)).method(method, body).header("Accept", "application/json").build()
        api.http.newCall(req).execute().use { resp ->
            val text = resp.body?.string().orEmpty()
            if (!resp.isSuccessful) {
                val e = ArrException.parse(resp.code, text)
                if (resp.code == 401 && e.code == "unauthenticated" && api.token != null) api.onUnauthorized?.invoke()
                throw e
            }
            if (text.isBlank()) null else runCatching { json.parseToJsonElement(text) }.getOrElse { JsonPrimitive(text) }
        }
    }

    companion object {
        val JSON = "application/json".toMediaType()

        fun imageUrl(api: Api, service: ArrService, url: String?): String? {
            if (url.isNullOrBlank()) return null
            val i = url.indexOf("MediaCover/")
            if (i >= 0) {
                // The path may carry the service's URL base (`/radarr/MediaCover/...`); the proxy wants it relative.
                return api.base.toString().trimEnd('/') + "/api/v1/arr/${service.id}/proxy/" + url.substring(i)
            }
            return if (url.startsWith("http")) url else null
        }

        suspend fun services(api: Api): List<ArrServiceStatus> = api.get<ArrServices>("arr/services").services
    }
}

private val JsonNull: JsonElement = kotlinx.serialization.json.JsonNull
