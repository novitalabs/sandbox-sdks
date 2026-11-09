package ai.novita.sandbox.core;

import okhttp3.mockwebserver.MockResponse;
import okhttp3.mockwebserver.MockWebServer;
import okio.Buffer;
import org.junit.jupiter.api.*;
import java.io.InputStream;
import java.util.Arrays;
import java.util.concurrent.*;
import java.util.concurrent.atomic.*;
import static org.junit.jupiter.api.Assertions.*;

/** Regressions from independent review of the protobuf watch wire and streaming uploads. */
class FilesystemStreamLifecycleTest {
    private MockWebServer server;
    private Sandbox sandbox;
    @BeforeEach void setup() throws Exception {
        server=new MockWebServer();server.start();
        server.enqueue(new MockResponse().setBody("{\"sandboxID\":\"fixture\",\"envdVersion\":\"0.5.7\"}"));
        SandboxOptions opts=new SandboxOptions().apiKey("local-placeholder").domain("modern.invalid")
                .baseUrl(server.url("/").toString()).sandboxUrl(server.url("/").toString()).requestTimeoutSeconds(3);
        sandbox=Sandbox.connect("fixture",opts);server.takeRequest();
    }
    @AfterEach void cleanup() throws Exception {sandbox.rpc().close();sandbox.getClient().close();server.shutdown();}

    @Test void watchAcceptsTopLevelProtobufOneofFields() throws Exception {
        Buffer body=RuntimeParityTest.frame(0,"{\"start\":{}}");
        body.writeAll(RuntimeParityTest.frame(0,"{\"keepalive\":{}}"));
        body.writeAll(RuntimeParityTest.frame(0,"{\"filesystem\":{\"name\":\"created.txt\",\"type\":\"EVENT_TYPE_CREATE\"}}"));
        body.writeAll(RuntimeParityTest.frame(2,"{}"));
        server.enqueue(new MockResponse().setHeader("Content-Type","application/connect+json").setBody(body));
        BlockingQueue<FilesystemEvent> events=new LinkedBlockingQueue<>();
        try(WatchHandle handle=sandbox.files().watch("/tmp",false,events::add,new FilesystemRequestOptions().setRequestTimeoutSeconds(1))){
            FilesystemEvent event=events.poll(1,TimeUnit.SECONDS);assertNotNull(event);assertEquals("created.txt",event.getName());
            assertEquals(FilesystemEvent.EventType.CREATE,event.getEventType());
            assertNull(handle.getFailure());assertTrue(events.isEmpty());
        }
    }

    @Test void gzipUploadTimeoutStopsResponsiveSourceAndPreservesOwnership() throws Exception {
        ResponsiveInput input=new ResponsiveInput();enqueueUploadResponse();
        CompletableFuture<Throwable> work=CompletableFuture.supplyAsync(()->{
            try{sandbox.files().writeStream("/a",input,new FilesystemWriteOptions().setUseOctetStream(true).setGzip(true).setRequestTimeoutSeconds(1));return null;}
            catch(Throwable error){return error;}
        });
        try {
            Throwable failure=work.get(2200,TimeUnit.MILLISECONDS);
            assertInstanceOf(ai.novita.sandbox.core.TimeoutException.class,failure);
            assertTrue(input.reads.get()>0);int finalReads=input.reads.get();Thread.sleep(50);assertEquals(finalReads,input.reads.get());assertFalse(input.closed.get());
        } finally {input.stop.set(true);work.get(2,TimeUnit.SECONDS);}
    }

    @Test void gzipUploadCancellationWithoutInterruptStopsResponsiveSource() throws Exception {
        ResponsiveInput input=new ResponsiveInput();enqueueUploadResponse();CountDownLatch finished=new CountDownLatch(1);
        CompletableFuture<Void> work=AsyncSupport.runAsync(()->{
            try{sandbox.files().writeStream("/a",input,new FilesystemWriteOptions().setUseOctetStream(true).setGzip(true).setRequestTimeoutSeconds(0));}
            catch(SandboxException error){throw new CompletionException(error);}
            finally{finished.countDown();}
        });
        try {
            assertTrue(input.started.await(1,TimeUnit.SECONDS));assertTrue(work.cancel(false));
            assertTrue(finished.await(1,TimeUnit.SECONDS),"cancel(false) must stop reading the gzip source");
            int finalReads=input.reads.get();Thread.sleep(50);assertEquals(finalReads,input.reads.get());assertFalse(input.closed.get());
        } finally {input.stop.set(true);assertTrue(finished.await(2,TimeUnit.SECONDS));}
    }

    private void enqueueUploadResponse(){server.enqueue(new MockResponse().setBody("[{\"path\":\"/a\"}]"));}
    private static final class ResponsiveInput extends InputStream {
        final AtomicBoolean stop=new AtomicBoolean(),closed=new AtomicBoolean();
        final AtomicInteger reads=new AtomicInteger();final CountDownLatch started=new CountDownLatch(1);
        @Override public int read(){return stop.get()?-1:0;}
        @Override public int read(byte[] bytes,int offset,int length){
            if(stop.get())return -1;reads.incrementAndGet();started.countDown();Arrays.fill(bytes,offset,offset+length,(byte)0);
            try{Thread.sleep(3);}catch(InterruptedException ignored){ }
            return length;
        }
        @Override public void close(){closed.set(true);}
    }
}
