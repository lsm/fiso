package io.fiso.worker

import com.fasterxml.jackson.databind.ObjectMapper
import io.temporal.activity.ActivityInterface
import io.temporal.activity.ActivityMethod
import java.net.URI
import java.net.http.HttpClient
import java.net.http.HttpRequest
import java.net.http.HttpResponse
import java.util.logging.Logger

/** Activity interface for calling external services through fiso-link. */
@ActivityInterface
interface ExternalServiceActivity {
    @ActivityMethod
    fun callExternalService(input: ByteArray): String
}

/**
 * Activity implementation that calls an external API through fiso-link.
 *
 * The input is a CloudEvent-wrapped payload from fiso-flow.
 * We extract the data field and forward it via the fiso-link proxy.
 */
class ExternalServiceActivityImpl(private val linkAddr: String) : ExternalServiceActivity {

    private val logger: Logger = Logger.getLogger(this::class.java.name)
    private val mapper = ObjectMapper()
    private val httpClient: HttpClient = HttpClient.newHttpClient()

    override fun callExternalService(input: ByteArray): String {
        val cloudEvent = mapper.readTree(input)
        val data = mapper.writeValueAsString(cloudEvent["data"])
        logger.info("calling fiso-link with data: $data")

        val request = HttpRequest.newBuilder()
            .uri(URI.create("$linkAddr/link/echo"))
            .header("Content-Type", "application/json")
            .POST(HttpRequest.BodyPublishers.ofString(data))
            .build()

        val response = httpClient.send(request, HttpResponse.BodyHandlers.ofString())
        val body = response.body()
        logger.info("WORKFLOW_COMPLETE external_response=$body")

        if (response.statusCode() >= 400) {
            throw RuntimeException("fiso-link returned ${response.statusCode()}: $body")
        }

        return body
    }
}
