package ai.novita.sandbox.core;

import com.google.gson.Gson;
import okhttp3.*;
import java.io.*; import java.nio.ByteBuffer; import java.time.Duration; import java.time.Instant; import java.util.function.Consumer;

/** Minimal Connect JSON transport for envd unary and server-streaming RPCs. */
public final class ConnectRpcClient implements AutoCloseable {
    /**
     * Dedicated pool for stream readers. These tasks block on socket reads, so
     * they must not occupy {@link java.util.concurrent.ForkJoinPool#commonPool()}.
     * Threads are daemon so an abandoned stream cannot keep the JVM alive.
     */
    private static final java.util.concurrent.ExecutorService STREAM_POOL =
            java.util.concurrent.Executors.newCachedThreadPool(new java.util.concurrent.ThreadFactory() {
                private final java.util.concurrent.atomic.AtomicInteger seq = new java.util.concurrent.atomic.AtomicInteger();
                @Override public Thread newThread(Runnable r) {
                    Thread t = new Thread(r, "novita-connect-stream-" + seq.incrementAndGet());
                    t.setDaemon(true);
                    return t;
                }
            });

    private final String baseUrl;
    /** Client for unary calls: bounded by {@code callTimeout}. */
    private final OkHttpClient http;
    /**
     * Client for server-streaming calls. A stream has no natural end (a directory
     * watch runs until cancelled), so it must not carry a call timeout.
     */
    private final OkHttpClient streamHttp;
    // Gson 2.10+ cannot reflect into java.time.Instant on Java  module boundaries.
    // envd filesystem responses contain timestamps, so register an explicit wire adapter.
    private final Gson gson = new com.google.gson.GsonBuilder()
            .registerTypeAdapter(Instant.class, (com.google.gson.JsonDeserializer<Instant>) (json, type, context) ->
                    json == null || json.isJsonNull() ? null : Instant.parse(json.getAsString()))
            .create();
    private Headers headers;
    private String accessToken;
    public ConnectRpcClient(String baseUrl, String accessToken, String sandboxId, Duration timeout) {
        this(baseUrl, accessToken, timeout);
        Headers.Builder builder = this.headers.newBuilder()
                .add("Novita-Sandbox-Id", sandboxId)
                .add("Novita-Sandbox-Port", "49983");
        this.headers = builder.build();
    }
    public ConnectRpcClient(String baseUrl, String accessToken, String sandboxId, Duration timeout, java.util.Map<String,String> extraHeaders) {
        this(baseUrl,accessToken,sandboxId,timeout,extraHeaders,null);
    }
    public ConnectRpcClient(String baseUrl, String accessToken, String sandboxId, Duration timeout, java.util.Map<String,String> extraHeaders, Consumer<String> logger) {
        this(baseUrl, accessToken, timeout, logger);
        headers=headers.newBuilder().set("Novita-Sandbox-Id",sandboxId).set("Novita-Sandbox-Port","49983").build();
        Headers.Builder b = Headers.of(extraHeaders).newBuilder();
        for (String name : headers.names()) b.set(name, headers.get(name));
        headers = b.build();
    }
    public ConnectRpcClient(String baseUrl, String accessToken) { this(baseUrl, accessToken, Duration.ofSeconds(60)); }

    public ConnectRpcClient(String baseUrl, String accessToken, Duration timeout) {
        this(baseUrl,accessToken,timeout,null);
    }
    private ConnectRpcClient(String baseUrl, String accessToken, Duration timeout, Consumer<String> logger) {
        this.baseUrl = baseUrl.replaceAll("/+$", "");
        this.accessToken = accessToken;
        OkHttpClient.Builder unaryBuilder = new OkHttpClient.Builder().callTimeout(timeout).readTimeout(timeout).writeTimeout(timeout).connectTimeout(timeout);
        OkHttpClient.Builder streamBuilder = new OkHttpClient.Builder()
                .connectTimeout(timeout)
                .readTimeout(0, java.util.concurrent.TimeUnit.MILLISECONDS)
                .writeTimeout(0, java.util.concurrent.TimeUnit.MILLISECONDS);
        configureProxy(unaryBuilder);
        configureProxy(streamBuilder);
        ApiClient.configureLogging(unaryBuilder,logger);
        ApiClient.configureLogging(streamBuilder,logger);
        this.http = unaryBuilder.build();
        this.streamHttp = streamBuilder.build();
        // Do NOT set Accept-Encoding here. OkHttp only transparently decompresses
        // gzip when it added the header itself; setting it manually disables that
        // and leaves the caller with raw gzip bytes.
        Headers.Builder headerBuilder = new Headers.Builder()
                .add("Connect-Protocol-Version", "1");
        if (accessToken != null && !accessToken.isEmpty()) {
            headerBuilder.add("X-Access-Token", accessToken);
        }
        this.headers = headerBuilder.build();
    }

    private static void configureProxy(OkHttpClient.Builder builder) {
        // Shared with ApiClient: applies HTTP(S)_PROXY while honoring NO_PROXY
        // (loopback and listed hosts stay direct), like the JS/Python stacks.
        ApiClient.configureProxy(builder);
    }

    public <T> T call(String service,String method,Object request,Class<T> type) throws SandboxException {
        return call(service, method, request, type, java.util.Collections.emptyMap());
    }
    public <T> T call(String service, String method, Object request, Class<T> type, java.util.Map<String,String> requestHeaders) throws SandboxException {
        return call(service, method, request, type, requestHeaders, null);
    }
    public <T> T call(String service, String method, Object request, Class<T> type,
                      java.util.Map<String,String> requestHeaders, Integer timeoutSeconds) throws SandboxException {
        Request r = new Request.Builder().url(baseUrl + "/" + service + "/" + method).headers(headersFor(requestHeaders))
                .post(RequestBody.create(gson.toJson(request), MediaType.get("application/json"))).build();
        try (Response x = AsyncSupport.execute(requestClient(timeoutSeconds).newCall(r))) {
            String body = x.body() == null ? "" : x.body().string();
            if (!x.isSuccessful()) {
                ConnectRpcException error = protocolError(body.getBytes(java.nio.charset.StandardCharsets.UTF_8), x.code());
                if (error.getCode() != null) throw error;
                throw ApiExceptions.from(x.code(), body);
            }
            return type == null ? null : gson.fromJson(body, type);
        } catch (IOException e) { throw transportFailure("Connect RPC failed", e); }
    }
    static SandboxException transportFailure(String message, IOException e) {
        if (e instanceof java.io.InterruptedIOException) return new TimeoutException(message + ": " + e.getMessage(), e);
        return new SandboxException(message, e);
    }
    /** Performs the envd REST file operation on the same authenticated endpoint. */
    public byte[] http(String method, String path, java.util.Map<String,String> query, byte[] data, String contentType) throws SandboxException {
        return http(method,path,query,data,contentType,false);
    }
    public byte[] http(String method, String path, java.util.Map<String,String> query, byte[] data, String contentType, boolean contentGzip) throws SandboxException {
        return http(method, path, query, data, contentType, contentGzip, null);
    }
    public byte[] http(String method, String path, java.util.Map<String,String> query, byte[] data, String contentType, boolean contentGzip, Integer timeoutSeconds) throws SandboxException {
        StringBuilder u = new StringBuilder(baseUrl).append(path);
        if (query != null && !query.isEmpty()) { u.append('?'); boolean first = true; for (java.util.Map.Entry<String,String> e : query.entrySet()) { if (!first) u.append('&'); first = false; u.append(java.net.URLEncoder.encode(e.getKey(), java.nio.charset.StandardCharsets.UTF_8)).append('=').append(java.net.URLEncoder.encode(e.getValue(), java.nio.charset.StandardCharsets.UTF_8)); } }
        Request.Builder b = new Request.Builder().url(u.toString()).headers(headers);
        if (contentGzip) b.header("Content-Encoding", "gzip");
        if (data != null) b.method(method, RequestBody.create(data, MediaType.get(contentType))); else b.method(method, null);
        for (int attempt = 0; attempt < 4; attempt++) {
            try (Response r = AsyncSupport.execute(requestClient(timeoutSeconds).newCall(b.build()))) {
                if (!r.isSuccessful()) {
                    String detail = r.body() == null ? r.message() : r.body().string();
                    if (r.code() == 502 && detail.contains("sandbox was not found") && attempt < 3) { try { Thread.sleep(1000L * (attempt + 1)); } catch (InterruptedException e) { Thread.currentThread().interrupt(); } continue; }
                    throw ApiExceptions.from(r.code(), detail);
                }
                return r.body() == null ? new byte[0] : r.body().bytes();
            } catch (IOException e) {
                if (e instanceof java.io.InterruptedIOException || AsyncSupport.isCancelled() || timeoutSeconds != null || attempt == 3)
                    throw transportFailure("envd HTTP request failed", e);
                try { Thread.sleep(1000L * (attempt + 1)); } catch (InterruptedException x) { Thread.currentThread().interrupt(); throw new SandboxException("envd HTTP request interrupted", x); }
            }
        }
        throw new SandboxException("envd HTTP request failed");
    }
    public byte[] multipart(String path, java.util.Map<String,String> query, String filename, byte[] data) throws SandboxException {
        return multipart(path, query, java.util.Collections.singletonList(new WriteEntry(filename, data)));
    }
    public byte[] multipart(String path, java.util.Map<String,String> query, java.util.List<WriteEntry> files) throws SandboxException {
        return multipart(path, query, files, null);
    }
    public byte[] multipart(String path, java.util.Map<String,String> query, java.util.List<WriteEntry> files, Integer timeoutSeconds) throws SandboxException {
        StringBuilder u = new StringBuilder(baseUrl).append(path);
        if (query != null && !query.isEmpty()) { u.append('?'); boolean first=true; for (java.util.Map.Entry<String,String> e:query.entrySet()) { if(!first)u.append('&'); first=false; u.append(java.net.URLEncoder.encode(e.getKey(),java.nio.charset.StandardCharsets.UTF_8)).append('=').append(java.net.URLEncoder.encode(e.getValue(),java.nio.charset.StandardCharsets.UTF_8)); } }
        MultipartBody.Builder multipart = new MultipartBody.Builder().setType(MultipartBody.FORM);
        for (WriteEntry file : files) multipart.addFormDataPart("file", file.getPath(), RequestBody.create(file.getData(), MediaType.get("application/octet-stream")));
        RequestBody body = multipart.build();
        try (Response r=AsyncSupport.execute(requestClient(timeoutSeconds).newCall(new Request.Builder().url(u.toString()).headers(headers).post(body).build()))) { if(!r.isSuccessful()) throw ApiExceptions.from(r.code(),r.body()==null?r.message():r.body().string()); return r.body()==null?new byte[0]:r.body().bytes(); } catch(IOException e){throw transportFailure("envd multipart request failed",e);}
    }
    private String fileUrl(String path, java.util.Map<String,String> query) {
        HttpUrl.Builder url = HttpUrl.get(baseUrl + path).newBuilder();
        if(query!=null)query.forEach(url::addQueryParameter);
        return url.build().toString();
    }
    private OkHttpClient requestClient(Integer seconds) {
        if(seconds==null)return http;
        return http.newBuilder().callTimeout(seconds,java.util.concurrent.TimeUnit.SECONDS)
                .connectTimeout(seconds,java.util.concurrent.TimeUnit.SECONDS).readTimeout(seconds,java.util.concurrent.TimeUnit.SECONDS)
                .writeTimeout(seconds,java.util.concurrent.TimeUnit.SECONDS).build();
    }
    /** Streams a file. Closing the returned stream releases its HTTP response. */
    public InputStream httpStream(String path, java.util.Map<String,String> query, Integer timeoutSeconds) throws SandboxException {
        Request request = new Request.Builder().url(fileUrl(path,query)).headers(headers).get().build();
        Response response=null;
        try {
            response=AsyncSupport.execute(requestClient(timeoutSeconds).newCall(request));
            if(!response.isSuccessful())throw ApiExceptions.from(response.code(),response.body()==null?response.message():response.body().string());
            final Response owned=response;
            InputStream input=response.body()==null?new ByteArrayInputStream(new byte[0]):response.body().byteStream();
            return new FilterInputStream(input){@Override public void close()throws IOException{try{super.close();}finally{owned.close();}}};
        }catch(IOException|SandboxException e){if(response!=null)response.close();if(e instanceof SandboxException)throw (SandboxException)e;throw transportFailure("File stream request failed",(IOException)e);}
    }
    /** Writes the caller-owned stream once, without buffering the full upload. */
    public byte[] uploadStream(String path, java.util.Map<String,String> query, String filename, InputStream input,
                               boolean octet, boolean gzip, Integer timeoutSeconds) throws SandboxException {
        final java.util.concurrent.atomic.AtomicReference<Call> activeCall=new java.util.concurrent.atomic.AtomicReference<>();
        RequestBody data=new RequestBody(){
            @Override public MediaType contentType(){return MediaType.get("application/octet-stream");}
            @Override public boolean isOneShot(){return true;}
            @Override public void writeTo(okio.BufferedSink sink)throws IOException{
                if(gzip&&octet){
                    // Dispose the compressor on failure without closing the
                    // transport's sink or the caller-owned input stream.
                    OutputStream unclosedSink=new FilterOutputStream(sink.outputStream()){
                        @Override public void write(byte[] bytes,int offset,int length)throws IOException{out.write(bytes,offset,length);}
                        @Override public void close()throws IOException{flush();}
                    };
                    try(java.util.zip.GZIPOutputStream output=new java.util.zip.GZIPOutputStream(unclosedSink)){
                        copyUpload(input,output,activeCall.get());
                    }
                }else{copyUpload(input,sink.outputStream(),activeCall.get());}
            }
        };
        RequestBody body=octet?data:new MultipartBody.Builder().setType(MultipartBody.FORM).addFormDataPart("file",filename,data).build();
        Request.Builder builder=new Request.Builder().url(fileUrl(path,query)).headers(headers).post(body);
        if(gzip&&octet)builder.header("Content-Encoding","gzip");
        Call call=requestClient(timeoutSeconds).newCall(builder.build());activeCall.set(call);
        try(Response response=AsyncSupport.execute(call)){
            if(!response.isSuccessful())throw ApiExceptions.from(response.code(),response.body()==null?response.message():response.body().string());
            return response.body()==null?new byte[0]:response.body().bytes();
        }catch(IOException e){throw transportFailure("File stream upload failed",e);}
    }
    private static void copyUpload(InputStream input,OutputStream output,Call call)throws IOException{
        byte[] buffer=new byte[8192];
        while(true){
            checkUploadCall(call);
            int count=input.read(buffer);
            checkUploadCall(call);
            if(count==-1)return;
            output.write(buffer,0,count);
        }
    }
    private static void checkUploadCall(Call call)throws IOException{
        // Gzip can consume many input buffers without writing compressed bytes.
        // Socket writes alone therefore cannot observe cancellation promptly.
        if(call.isCanceled())throw new IOException("File stream upload cancelled");
        if(Thread.currentThread().isInterrupted())throw new InterruptedIOException("File stream upload interrupted");
    }
    public void stream(String service,String method,Object request,Consumer<byte[]> consumer) throws SandboxException {
        stream(service, method, request, consumer, java.util.Collections.emptyMap());
    }
    public void stream(String service,String method,Object request,Consumer<byte[]> consumer,java.util.Map<String,String> requestHeaders) throws SandboxException { byte[] payload=gson.toJson(request).getBytes(java.nio.charset.StandardCharsets.UTF_8); byte[] frame=ByteBuffer.allocate(5+payload.length).put((byte)0).putInt(payload.length).put(payload).array(); Request r=new Request.Builder().url(baseUrl+"/"+service+"/"+method).headers(headersFor(requestHeaders)).header("Accept","application/connect+json").post(RequestBody.create(frame,MediaType.get("application/connect+json"))).build(); try(Response x=AsyncSupport.execute(streamHttp.newCall(r))){if(!x.isSuccessful()){String body=x.body()==null?"":x.body().string();throw ApiExceptions.from(x.code(),body);} ResponseBody rb=x.body(); if(rb==null)return; readFrames(rb.byteStream(),consumer);}catch(SandboxException e){throw e;}catch(IOException e){throw transportFailure("Connect stream failed: " + e.getMessage(),e);} }
    private static boolean isEmptyTrailer(byte[] bytes) {
        String s = new String(bytes, java.nio.charset.StandardCharsets.UTF_8).trim();
        return s.isEmpty() || "{}".equals(s);
    }

    private ConnectRpcException protocolError(byte[] bytes) { return protocolError(bytes,null); }
    private ConnectRpcException protocolError(byte[] bytes, Integer fallbackStatus) {
        String raw = new String(bytes, java.nio.charset.StandardCharsets.UTF_8);
        try {
            com.google.gson.JsonObject o = com.google.gson.JsonParser.parseString(raw).getAsJsonObject();
            String code = o.has("code") ? o.get("code").getAsString() : null;
            String message = o.has("message") ? o.get("message").getAsString() : null;
            Integer status = fallbackStatus;
            if (o.has("httpStatus")) status = Integer.valueOf(o.get("httpStatus").getAsInt());
            if (message == null || message.isEmpty()) message = raw;
            return new ConnectRpcException(code, message, status);
        } catch(Exception e) { return new ConnectRpcException(null, raw, fallbackStatus); }
    }
    public java.util.concurrent.CompletableFuture<Void> streamAsync(String service,String method,Object request,Consumer<byte[]> consumer) { return AsyncSupport.runAsync(() -> { try { stream(service,method,request,consumer); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } }); }
    /** Starts a stream and returns a handle that can cancel the underlying OkHttp call. */
    public StreamHandle streamCancellable(String service,String method,Object request,Consumer<byte[]> consumer) {
        return streamCancellable(service, method, request, consumer, java.util.Collections.emptyMap());
    }
    public StreamHandle streamCancellable(String service,String method,Object request,Consumer<byte[]> consumer,java.util.Map<String,String> requestHeaders) {
        return streamCancellable(service, method, request, consumer, requestHeaders, 0, http.connectTimeoutMillis() / 1000);
    }
    public StreamHandle streamCancellable(String service, String method, Object request, Consumer<byte[]> consumer,
                                         java.util.Map<String,String> requestHeaders, int timeoutSeconds, int requestTimeoutSeconds) {
        byte[] payload=gson.toJson(request).getBytes(java.nio.charset.StandardCharsets.UTF_8);
        byte[] frame=ByteBuffer.allocate(5+payload.length).put((byte)0).putInt(payload.length).put(payload).array();
        Request r=new Request.Builder().url(baseUrl+"/"+service+"/"+method).headers(headersFor(requestHeaders)).header("Accept","application/connect+json").post(RequestBody.create(frame,MediaType.get("application/connect+json"))).build();
        Call call=AsyncSupport.track(streamHttp.newBuilder().callTimeout(timeoutSeconds, java.util.concurrent.TimeUnit.SECONDS)
                .connectTimeout(requestTimeoutSeconds, java.util.concurrent.TimeUnit.SECONDS).readTimeout(requestTimeoutSeconds, java.util.concurrent.TimeUnit.SECONDS).build().newCall(r));
        java.util.concurrent.CompletableFuture<Void> future=new java.util.concurrent.CompletableFuture<Void>(){
            @Override public boolean cancel(boolean interrupt){boolean cancelled=super.cancel(interrupt);if(cancelled)call.cancel();return cancelled;}
        };
        STREAM_POOL.execute(()->{
            try(Response response=call.execute()){
                if(!response.isSuccessful())throw ApiExceptions.from(response.code(),response.body()==null?response.message():response.body().string());
                ResponseBody body=response.body();
                if(body!=null){body.source().timeout().clearTimeout();readFrames(body.byteStream(),bytes->{if(call.isCanceled())throw new java.util.concurrent.CancellationException("Connect stream cancelled");consumer.accept(bytes);});}
                future.complete(null);
            }catch(Exception e){future.completeExceptionally(e);}
        });
        return new StreamHandle(call,future);
    }
    private Headers headersFor(java.util.Map<String,String> requestHeaders) {
        if (requestHeaders == null || requestHeaders.isEmpty()) return headers;
        Headers.Builder builder = headers.newBuilder();
        for (java.util.Map.Entry<String,String> entry : requestHeaders.entrySet()) {
            if (entry.getValue() != null) builder.set(entry.getKey(), entry.getValue());
        }
        return builder.build();
    }
    public static final class StreamHandle { private final Call call; private final java.util.concurrent.CompletableFuture<Void> future; private StreamHandle(Call c,java.util.concurrent.CompletableFuture<Void> f){call=c;future=f;} public void cancel(){call.cancel();future.cancel(true);} public boolean isCancelled(){return call.isCanceled()||future.isCancelled();} public java.util.concurrent.CompletableFuture<Void> future(){return future;} }
    private void readFrames(InputStream in, Consumer<byte[]> consumer) throws IOException, ConnectRpcException {
        byte[] header = new byte[5];
        while (true) {
            AsyncSupport.throwIfCancelled();
            int headerBytes = read(in, header);
            if (headerBytes == 0) return;
            if (headerBytes != header.length) throw new EOFException("truncated Connect frame header");
            int flags = header[0] & 255;
            int length = ByteBuffer.wrap(header, 1, 4).getInt();
            if (length < 0 || length > 64 * 1024 * 1024) throw new IOException("invalid Connect frame length");
            byte[] body = new byte[length];
            if (read(in, body) != length) throw new EOFException("truncated Connect frame body");
            if ((flags & 2) != 0) {
                if ((flags & ~2) != 0) throw new IOException("invalid Connect end-stream flags");
                if (!isEmptyTrailer(body)) {
                    try {
                        com.google.gson.JsonObject trailer = com.google.gson.JsonParser.parseString(new String(body, java.nio.charset.StandardCharsets.UTF_8)).getAsJsonObject();
                        if (trailer.has("error") && !trailer.get("error").isJsonNull())
                            throw protocolError(trailer.get("error").toString().getBytes(java.nio.charset.StandardCharsets.UTF_8));
                        // Retain compatibility with servers emitting a flat error trailer.
                        if (trailer.has("code")) throw protocolError(body);
                    } catch (com.google.gson.JsonParseException | IllegalStateException e) { throw new IOException("invalid Connect end-stream envelope", e); }
                }
                return;
            }
            if ((flags & 1) != 0) throw new IOException("compressed Connect frame received without negotiated encoding");
            if ((flags & ~3) != 0) throw new IOException("unsupported Connect frame flags: " + flags);
            consumer.accept(body);
        }
    }
    private static int read(InputStream in,byte[] b)throws IOException{int o=0,n;while(o<b.length&&(n=in.read(b,o,b.length-o))>0)o+=n;return o;}

    /** Releases the connection pools and dispatcher threads held by both clients. */
    @Override public void close() {
        shutdown(http);
        shutdown(streamHttp);
    }

    private static void shutdown(OkHttpClient client) {
        client.dispatcher().cancelAll();
        client.dispatcher().executorService().shutdown();
        client.connectionPool().evictAll();
    }
}
