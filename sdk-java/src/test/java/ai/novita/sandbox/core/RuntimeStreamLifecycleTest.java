package ai.novita.sandbox.core;

import ai.novita.sandbox.code_interpreter.*;
import okhttp3.mockwebserver.*;
import org.junit.jupiter.api.Test;
import java.io.*;
import java.net.*;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.*;
import java.util.concurrent.atomic.*;
import static org.junit.jupiter.api.Assertions.*;

/** A real loopback peer verifies cancellation closes the socket, not just the future. */
class RuntimeStreamLifecycleTest {
    private SandboxOptions options(String url){return new SandboxOptions().apiKey("local-placeholder").domain("modern.example").baseUrl(url).sandboxUrl(url).requestTimeoutSeconds(5);}
    private Sandbox connect(MockWebServer control,String runtime)throws Exception{
        control.enqueue(new MockResponse().setBody("{\"sandboxID\":\"sb\",\"envdVersion\":\"0.5.7\"}"));
        return Sandbox.connect("sb",options(control.url("/").toString()).sandboxUrl(runtime));
    }
    @Test void processEndFinishesWithoutWaitingForHttpEof()throws Exception{
        okio.Buffer body=RuntimeParityTest.frame(0,"{\"event\":{\"start\":{\"pid\":42}}}");
        body.writeAll(RuntimeParityTest.frame(0,"{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}"));
        try(OpenBodyPeer peer=new OpenBodyPeer(body.readByteArray());MockWebServer control=new MockWebServer()){
            control.start();Sandbox s=connect(control,peer.url());
            CompletableFuture<CommandResult> future=s.commands().runAsync("true",null);
            try{assertEquals(0,future.get(1,TimeUnit.SECONDS).getExitCode());assertTrue(peer.closed.await(1,TimeUnit.SECONDS),"End must release the response socket");}
            finally{future.cancel(true);s.rpc().close();s.getClient().close();}
        }
    }
    @Test void templateStatusFutureCancellationClosesHttpSocket()throws Exception{
        try(OpenBodyPeer peer=new OpenBodyPeer(new byte[0]);Novita sdk=Novita.create(options(peer.url()))){
            CompletableFuture<BuildStatus> future=sdk.templates().getBuildStatusAsync("template","build");
            assertTrue(peer.request.await(2,TimeUnit.SECONDS));assertTrue(future.cancel(true));
            assertTrue(peer.closed.await(1,TimeUnit.SECONDS),"Template future must cancel its actual HTTP call");
        }
    }
    @Test void gitFutureCancellationClosesProcessStream()throws Exception{
        try(OpenBodyPeer peer=new OpenBodyPeer(RuntimeParityTest.frame(0,"{\"event\":{\"start\":{\"pid\":42}}}").readByteArray());MockWebServer control=new MockWebServer()){
            control.start();Sandbox s=connect(control,peer.url());CompletableFuture<GitStatus> future=s.git().statusAsync();
            try{assertTrue(peer.request.await(2,TimeUnit.SECONDS));assertTrue(future.cancel(true));assertTrue(peer.closed.await(1,TimeUnit.SECONDS),"Git future must cancel its process HTTP call");}
            finally{future.cancel(true);s.rpc().close();s.getClient().close();}
        }
    }
    @Test void cancellingBuildDuringImageLookupClosesRegistrySocket()throws Exception{
        try(OpenBodyPeer registry=new OpenBodyPeer(new byte[0]);MockWebServer control=new MockWebServer()){
            control.start();try(Novita sdk=Novita.create(options(control.url("/").toString()))){
                String image="localhost:"+registry.listener.getLocalPort()+"/image:tag";
                CompletableFuture<BuildInfo> future=sdk.templates().buildInBackgroundAsync("test",new TemplateBuilder().fromImage(image),new TemplateBuildOptions());
                assertTrue(registry.request.await(2,TimeUnit.SECONDS));assertTrue(future.cancel(true));assertTrue(registry.closed.await(1,TimeUnit.SECONDS));
                assertNull(control.takeRequest(50,TimeUnit.MILLISECONDS),"A cancelled registry lookup must not allocate a build");
            }
        }
    }
    @Test void cancelledCodeExecutionDiscardsBufferedMessages()throws Exception{
        try(MockWebServer server=new MockWebServer()){
            server.start();Sandbox s=connect(server,server.url("/").toString());
            server.enqueue(new MockResponse().setBody("{\"type\":\"stdout\",\"text\":\"first\"}\n{\"type\":\"stdout\",\"text\":\"second\"}\n{\"type\":\"stdout\",\"text\":\"third\"}\n"));
            AtomicReference<CompletableFuture<ExecutionResult>> reference=new AtomicReference<>();CountDownLatch assigned=new CountDownLatch(1),cancelled=new CountDownLatch(1);AtomicInteger callbacks=new AtomicInteger();
            CompletableFuture<ExecutionResult> future=CodeInterpreterSandbox.wrap(s).runCodeAsync("x",new RunCodeOpts().setOnStdout(value->{
                callbacks.incrementAndGet();try{assertTrue(assigned.await(2,TimeUnit.SECONDS));}catch(InterruptedException e){throw new RuntimeException(e);}reference.get().cancel(true);cancelled.countDown();
            }));reference.set(future);assigned.countDown();
            try{assertTrue(cancelled.await(2,TimeUnit.SECONDS));long until=System.nanoTime()+TimeUnit.SECONDS.toNanos(1);while(s.httpClient().dispatcher().runningCallsCount()>0&&System.nanoTime()<until)Thread.sleep(10);assertEquals(1,callbacks.get());assertTrue(future.isCancelled());}
            finally{s.rpc().close();s.getClient().close();}
        }
    }
    private static final class OpenBodyPeer implements AutoCloseable {
        final ServerSocket listener;
        volatile Socket socket;
        final CountDownLatch request=new CountDownLatch(1),closed=new CountDownLatch(1);
        final Thread worker;
        OpenBodyPeer(byte[] body)throws IOException{
            listener=new ServerSocket(0,1,InetAddress.getLoopbackAddress());
            worker=new Thread(()->{
                try(Socket accepted=listener.accept()){
                    socket=accepted;InputStream input=accepted.getInputStream();ByteArrayOutputStream headers=new ByteArrayOutputStream();int matched=0,value;
                    byte[] end={'\r','\n','\r','\n'};
                    while(matched<4&&(value=input.read())!=-1){headers.write(value);matched=value==end[matched]?matched+1:(value=='\r'?1:0);}
                    String text=headers.toString(StandardCharsets.US_ASCII);int length=0;
                    for(String line:text.split("\r\n"))if(line.toLowerCase(java.util.Locale.ROOT).startsWith("content-length:"))length=Integer.parseInt(line.substring(line.indexOf(':')+1).trim());
                    for(int i=0;i<length;i++)if(input.read()==-1)throw new EOFException();request.countDown();
                    OutputStream output=accepted.getOutputStream();output.write("HTTP/1.1 200 OK\r\nContent-Type: application/connect+json\r\nContent-Length: 1000000\r\n\r\n".getBytes(StandardCharsets.US_ASCII));output.write(body);output.flush();
                    while(input.read()!=-1){ }
                }catch(IOException expectedDisconnect){ }
                finally{closed.countDown();}
            },"runtime-test-peer");worker.setDaemon(true);worker.start();
        }
        String url(){return "http://localhost:"+listener.getLocalPort();}
        @Override public void close()throws Exception{listener.close();if(socket!=null)socket.close();worker.join(1000);}
    }
}
