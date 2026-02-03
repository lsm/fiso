package io.fiso.worker;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.util.logging.Logger;

/**
 * Activity implementation that calls an external API through fiso-link.
 *
 * <p>The input is a CloudEvent-wrapped payload from fiso-flow. We extract the data field and
 * forward it via the fiso-link proxy.
 */
public class ExternalServiceActivityImpl implements ExternalServiceActivity {

    private static final Logger logger =
            Logger.getLogger(ExternalServiceActivityImpl.class.getName());
    private static final ObjectMapper mapper = new ObjectMapper();

    private final String linkAddr;
    private final HttpClient httpClient;

    public ExternalServiceActivityImpl(String linkAddr) {
        this.linkAddr = linkAddr;
        this.httpClient = HttpClient.newHttpClient();
    }

    @Override
    public String callExternalService(byte[] input) {
        try {
            JsonNode cloudEvent = mapper.readTree(input);
            String data = mapper.writeValueAsString(cloudEvent.get("data"));
            logger.info("calling fiso-link with data: " + data);

            HttpRequest request =
                    HttpRequest.newBuilder()
                            .uri(URI.create(linkAddr + "/link/echo"))
                            .header("Content-Type", "application/json")
                            .POST(HttpRequest.BodyPublishers.ofString(data))
                            .build();

            HttpResponse<String> response =
                    httpClient.send(request, HttpResponse.BodyHandlers.ofString());

            String body = response.body();
            logger.info("WORKFLOW_COMPLETE external_response=" + body);

            if (response.statusCode() >= 400) {
                throw new RuntimeException(
                        "fiso-link returned " + response.statusCode() + ": " + body);
            }

            return body;
        } catch (RuntimeException e) {
            throw e;
        } catch (Exception e) {
            throw new RuntimeException("fiso-link call failed", e);
        }
    }
}
