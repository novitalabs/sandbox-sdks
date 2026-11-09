package ai.novita.sandbox.core;

import com.google.gson.*;
import okhttp3.mockwebserver.*;
import org.junit.jupiter.api.Test;
import java.util.*;
import static org.junit.jupiter.api.Assertions.*;

class AlphaParityTest {
    @Test void longRunningOmitsCreationTimeoutAndPreservesExplicitIdle() throws Exception {
        for(boolean legacy:new boolean[]{false,true})for(String flag:Arrays.asList("true","false","TRUE",""))for(boolean idle:new boolean[]{false,true}){
            try(MockWebServer server=new MockWebServer()){
                server.start();server.enqueue(new MockResponse().setBody("{\"sandboxID\":\"sb\",\"envdVersion\":\"0.5.7\"}"));
                SandboxOpts opts=new SandboxOpts();opts.setApiKey("test");opts.setDomain(legacy?"sandbox.novita.ai":"us-phx-1.sandbox.novita.ai");opts.setBaseUrl(server.url("/").toString());opts.setTimeout(900);
                Map<String,String> metadata=new LinkedHashMap<>();metadata.put("long_running",flag);if(idle)metadata.put("idle_timeout","120");opts.setMetadata(metadata);
                try(ApiClient client=new ApiClient(opts)){client.createSandbox("base",opts);}
                JsonObject body=JsonParser.parseString(server.takeRequest().getBody().readUtf8()).getAsJsonObject();
                assertEquals(!"true".equals(flag),body.has("timeout"),body.toString());assertEquals(new Gson().toJsonTree(metadata),body.get("metadata"));assertEquals(900,opts.getTimeout());
            }
        }
    }
}
