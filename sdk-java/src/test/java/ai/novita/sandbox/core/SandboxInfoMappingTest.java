package ai.novita.sandbox.core;

import com.google.gson.Gson;
import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.assertEquals;

class SandboxInfoMappingTest {
    @Test
    void mapsLegacySnakeCaseConnectionFields() {
        SandboxInfo info = new Gson().fromJson(
                "{\"sandbox_id\":\"sb-1\",\"sandbox_domain\":\"us-phx-1.sandbox.novita.ai\","
                        + "\"envd_access_token\":\"envd-token\",\"client_id\":\"client-1\"}",
                SandboxInfo.class);

        assertEquals("sb-1", info.getId());
        assertEquals("us-phx-1.sandbox.novita.ai", info.getDomain());
        assertEquals("envd-token", info.getEnvdAccessToken());
        assertEquals("client-1", info.getClientID());
    }
}
