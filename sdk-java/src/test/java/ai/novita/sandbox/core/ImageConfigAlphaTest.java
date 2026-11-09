package ai.novita.sandbox.core;

import okhttp3.*;
import okhttp3.mockwebserver.*;
import org.junit.jupiter.api.Test;
import java.io.*;
import java.util.*;
import static org.junit.jupiter.api.Assertions.*;

class ImageConfigAlphaTest {
    @Test void successfulPinnedConfigIsReusedButClientChangeInvalidatesIt()throws Exception{
        try(MockWebServer server=new MockWebServer()){
            server.start();String image=server.getHostName()+":"+server.getPort()+"/image@sha256:fixed";
            server.enqueue(new MockResponse().setBody("{\"config\":{\"digest\":\"sha256:cfg\"}}"));server.enqueue(new MockResponse().setBody("{\"config\":{\"Cmd\":[\"start\"]}}"));
            TemplateBuilder b=TemplateBuilder.create().fromImage(image).onConfigFailure(ImageConfigFailurePolicy.IGNORE);String first=b.toJson();
            server.enqueue(new MockResponse().setResponseCode(404));assertEquals(first,b.toJson());assertEquals(2,server.getRequestCount());
            b.imageConfigHttpClient(new OkHttpClient());assertFalse(b.toJson().contains("startCmd"));assertNotNull(b.getImageConfigFailure());
        }
    }
    @Test void failurePoliciesWarnRaiseIgnoreAndRefreshDiagnostics()throws Exception{
        PrintStream original=System.err;ByteArrayOutputStream warnings=new ByteArrayOutputStream();System.setErr(new PrintStream(warnings));
        try(MockWebServer server=new MockWebServer()){
            server.start();String image=server.getHostName()+":"+server.getPort()+"/image";
            for(ImageConfigFailurePolicy policy:ImageConfigFailurePolicy.values()){
                warnings.reset();server.enqueue(new MockResponse().setResponseCode(404));TemplateBuilder b=TemplateBuilder.create().fromImage(image).onConfigFailure(policy);
                if(policy==ImageConfigFailurePolicy.RAISE)assertThrows(ImageConfigException.class,b::toJson);else b.toJson();
                assertNotNull(b.getImageConfigFailure());assertEquals(policy==ImageConfigFailurePolicy.WARN,warnings.size()>0);
            }
            TemplateBuilder b=TemplateBuilder.create().fromImage(image).onConfigFailure(ImageConfigFailurePolicy.IGNORE);
            server.enqueue(new MockResponse().setResponseCode(404));b.toJson();assertNotNull(b.getImageConfigFailure());
            server.enqueue(new MockResponse().setBody("{\"config\":{\"digest\":\"sha256:cfg\"}}"));server.enqueue(new MockResponse().setBody("{\"config\":{}}"));b.toJson();assertNull(b.getImageConfigFailure());
            assertThrows(IllegalArgumentException.class,()->b.onConfigFailure(null));
        }finally{System.setErr(original);}
    }
    @Test void customRegistryClientHandlesMixedChallengeAndTaggedDigest()throws Exception{
        List<Request> requests=new ArrayList<>();
        OkHttpClient transport=new OkHttpClient.Builder().addInterceptor(chain->{
            Request r=chain.request();requests.add(r);String path=r.url().encodedPath();int code=200;String body;String challenge=null;
            if(path.equals("/token")){assertEquals("repository:org/image:pull",r.url().queryParameter("scope"));assertEquals("registry,service",r.url().queryParameter("service"));body="{\"access_token\":\"bearer\"}";}
            else if(path.equals("/v2/org/image/manifests/sha256:pin")){
                if(!"Bearer bearer".equals(r.header("Authorization"))){code=401;body="{}";challenge="Basic realm=\"basic\", Bearer realm=\"https://registry.example/token\",service=\"registry,service\"";}
                else body="{\"config\":{\"digest\":\"sha256:cfg\"}}";
            }else{assertEquals("/v2/org/image/blobs/sha256:cfg",path);assertEquals("Bearer bearer",r.header("Authorization"));body="{\"config\":{\"WorkingDir\":\"/app\"}}";}
            Response.Builder response=new Response.Builder().request(r).protocol(Protocol.HTTP_1_1).code(code).message("fixture").body(ResponseBody.create(body,MediaType.get("application/json")));if(challenge!=null)response.header("WWW-Authenticate",challenge);return response.build();
        }).build();
        try{
            String json=TemplateBuilder.create().fromImage("registry.example/org/image:tag@sha256:pin").imageConfigHttpClient(transport).onConfigFailure(ImageConfigFailurePolicy.RAISE).toJson();
            assertTrue(json.contains("/app"));assertEquals(4,requests.size());assertFalse(json.contains("imageConfigHttpClient"));assertFalse(transport.dispatcher().executorService().isShutdown());
        }finally{transport.connectionPool().evictAll();transport.dispatcher().executorService().shutdown();}
    }
}
