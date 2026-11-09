package ai.novita.sandbox.core;
import java.util.Map;

/**
 * Filesystem operations for a sandbox.
 */
public class Filesystem {
    private final Sandbox sandbox;

    Filesystem(Sandbox sandbox) {
        this.sandbox = sandbox;
    }
    public java.util.concurrent.CompletableFuture<byte[]> readAsync(String path) { return AsyncSupport.supplyAsync(() -> { try{return read(path);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);} }); }
    public java.util.concurrent.CompletableFuture<byte[]> readAsync(String path, FilesystemReadOptions opts) { return AsyncSupport.supplyAsync(() -> { try{return read(path,opts);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);} }); }
    public java.util.concurrent.CompletableFuture<Void> writeAsync(String path, byte[] data, FilesystemWriteOpts opts) { return AsyncSupport.runAsync(() -> { try{write(path,data,opts);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);} }); }
    public java.util.concurrent.CompletableFuture<EntryInfo[]> listAsync(String path, int depth) { return AsyncSupport.supplyAsync(() -> { try{return list(path,depth);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);} }); }

    /**
     * Writes data to a file in the sandbox.
     *
     * @param path The file path
     * @param data The data to write
     * @param opts Optional write options
     * @throws SandboxException if the operation fails
     */
    public WriteInfo write(String path, byte[] data, FilesystemWriteOpts opts) throws SandboxException { return write(path,data,(FilesystemWriteOptions)opts); }
    public WriteInfo write(String path, byte[] data, FilesystemWriteOptions opts) throws SandboxException {
        WriteInfo[] result=writeFiles(java.util.Collections.singletonList(new WriteEntry(path,data)),opts);
        if(result.length!=1)throw new SandboxException("Received unexpected response from write operation");
        return result[0];
    }

    /**
     * Reads a file from the sandbox.
     *
     * @param path The file path
     * @return The file contents
     * @throws SandboxException if the operation fails
     */
    public byte[] read(String path) throws SandboxException {
        return read(path, null);
    }
    public byte[] read(String path, FilesystemReadOptions opts) throws SandboxException {
        return readWithOptions(path, opts);
    }
    public byte[] readWithOptions(String path, FilesystemReadOptions opts) throws SandboxException {
        try(java.io.InputStream input=readStream(path,opts)){return input.readAllBytes();}
        catch(java.io.IOException e){throw new SandboxException("File read failed",e);}
    }
    /** The caller must close the returned stream to release its network connection. */
    public java.io.InputStream readStream(String path) throws SandboxException {return readStream(path,null);}
    public java.io.InputStream readStream(String path, FilesystemReadOptions opts) throws SandboxException {
        java.util.Map<String,String> query=new java.util.LinkedHashMap<>();query.put("path",path);
        String username=filesystemUsername(opts==null?null:opts.getUsername());if(username!=null)query.put("username",username);
        if(opts!=null&&opts.getVersion()!=null)query.put("version",opts.getVersion());
        return sandbox.rpc().httpStream("/files",query,opts==null?null:opts.getRequestTimeoutSeconds());
    }
    /** Uploads without buffering; ownership of input remains with the caller. */
    public WriteInfo writeStream(String path, java.io.InputStream input) throws SandboxException {return writeStream(path,input,null);}
    public WriteInfo writeStream(String path, java.io.InputStream input, FilesystemWriteOptions opts) throws SandboxException {
        java.util.Objects.requireNonNull(input,"input");
        FilesystemWriteOptions effective=opts==null?new FilesystemWriteOptions():opts;
        java.util.Map<String,String> query=writeQuery(effective);query.put("path",path);
        boolean octet=effective.isUseOctetStream()&&sandbox.envdAtLeast("0.5.7");
        WriteInfo[] infos=parseWriteResponse(sandbox.rpc().uploadStream("/files",query,path,input,octet,effective.isGzip(),effective.getRequestTimeoutSeconds()));
        if(infos.length!=1)throw new SandboxException("Expected one written file");return infos[0];
    }

    public String readText(String path) throws SandboxException { return new String(read(path), java.nio.charset.StandardCharsets.UTF_8); }
    public void writeText(String path,String content,FilesystemWriteOpts opts) throws SandboxException { write(path,content.getBytes(java.nio.charset.StandardCharsets.UTF_8),opts); }
    public void writeWithOptions(String path, byte[] data, FilesystemWriteOptions opts) throws SandboxException { write(path,data,opts); }
    private String filesystemUsername(String requested) throws SandboxException { if(requested!=null)return requested; return sandbox.envdAtLeast("0.4.0")?null:"user"; }
    public WriteInfo[] writeFiles(java.util.List<WriteEntry> files, FilesystemWriteOptions opts) throws SandboxException { if(files==null||files.isEmpty())return new WriteInfo[0]; FilesystemWriteOptions options=opts==null?new FilesystemWriteOptions():opts; if(options.isUseOctetStream()&&sandbox.envdAtLeast("0.5.7")){WriteInfo[] out=new WriteInfo[files.size()];for(int i=0;i<files.size();i++)out[i]=writeOctet(files.get(i),options);return out;} return writeFilesMultipart(files,options); }
    private WriteInfo writeOctet(WriteEntry file, FilesystemWriteOptions opts) throws SandboxException { java.util.Map<String,String> q=writeQuery(opts);q.put("path",file.getPath());try{byte[] body=file.getData();if(opts.isGzip()){java.io.ByteArrayOutputStream b=new java.io.ByteArrayOutputStream();java.util.zip.GZIPOutputStream g=new java.util.zip.GZIPOutputStream(b);g.write(body);g.close();body=b.toByteArray();}byte[] result=sandbox.rpc().http("POST","/files",q,body,"application/octet-stream",opts.isGzip(),opts.getRequestTimeoutSeconds());WriteInfo[] infos=parseWriteResponse(result);return infos[0];}catch(java.io.IOException e){throw new SandboxException("gzip compression failed",e);} }
    public java.util.concurrent.CompletableFuture<WriteInfo[]> writeFilesAsync(java.util.List<WriteEntry> files, FilesystemWriteOptions opts){return AsyncSupport.supplyAsync(()->{try{return writeFiles(files,opts);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public WriteInfo[] writeFilesMultipart(java.util.List<WriteEntry> files) throws SandboxException { return writeFilesMultipart(files,new FilesystemWriteOptions()); }
    public WriteInfo[] writeFilesMultipart(java.util.List<WriteEntry> files, FilesystemWriteOptions opts) throws SandboxException { if(files==null||files.isEmpty())return new WriteInfo[0]; java.util.Map<String,String> q=writeQuery(opts);if(files.size()==1)q.put("path",files.get(0).getPath());return parseWriteResponse(sandbox.rpc().multipart("/files",q,files,opts==null?null:opts.getRequestTimeoutSeconds())); }
    private java.util.Map<String,String> writeQuery(FilesystemWriteOptions opts) throws SandboxException { java.util.Map<String,String> q=new java.util.LinkedHashMap<>();String username=filesystemUsername(opts==null?null:opts.getUsername());if(username!=null)q.put("username",username);if(opts!=null&&opts.getVersion()!=null)q.put("version",opts.getVersion());if(opts!=null&&opts.getMode()!=0644)q.put("mode",Integer.toString(opts.getMode()));return q; }
    private static WriteInfo[] parseWriteResponse(byte[] response) throws SandboxException { try{com.google.gson.JsonElement parsed=com.google.gson.JsonParser.parseString(new String(response,java.nio.charset.StandardCharsets.UTF_8));if(!parsed.isJsonArray()||parsed.getAsJsonArray().size()==0)throw new SandboxException("Expected to receive information about written file");return new com.google.gson.Gson().fromJson(parsed,WriteInfo[].class);}catch(com.google.gson.JsonParseException e){throw new SandboxException("Invalid filesystem write response",e);} }
    public java.util.concurrent.CompletableFuture<WriteInfo[]> writeFilesMultipartAsync(java.util.List<WriteEntry> files){return AsyncSupport.supplyAsync(()->{try{return writeFilesMultipart(files);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<String> readTextAsync(String path){return AsyncSupport.supplyAsync(()->{try{return readText(path);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<Void> writeTextAsync(String path,String content,FilesystemWriteOpts opts){return AsyncSupport.runAsync(()->{try{writeText(path,content,opts);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}

    /**
     * Lists files in a directory.
     *
     * @param path The directory path
     * @return Array of entry information
     * @throws SandboxException if the operation fails
     */
    public EntryInfo[] list(String path) throws SandboxException {
        return list(path, 1);
    }
    public EntryInfo[] list(String path, int depth) throws SandboxException {
        return list(path, depth, null);
    }
    public EntryInfo[] list(String path, int depth, FilesystemRequestOptions opts) throws SandboxException {
        java.util.Map<String,Object> p=new java.util.HashMap<>();p.put("path",path);p.put("depth",Math.max(1, depth));
        RawFilesResponse r=sandbox.rpc().call("filesystem.Filesystem","ListDir",p,RawFilesResponse.class,
                sandbox.envdAuthenticationHeaders(opts == null ? null : opts.getUser()), opts == null ? null : opts.getRequestTimeoutSeconds());
        if (r.entries == null) return new EntryInfo[0]; EntryInfo[] out=new EntryInfo[r.entries.length];
        for(int i=0;i<out.length;i++) out[i]=convert(r.entries[i]); return out;
    }

    /**
     * Removes a file or directory.
     *
     * @param path The path to remove
     * @throws SandboxException if the operation fails
     */
    public void remove(String path) throws SandboxException {
        remove(path, null);
    }
    public void remove(String path, FilesystemRequestOptions opts) throws SandboxException {
        sandbox.rpc().call("filesystem.Filesystem","Remove",java.util.Collections.singletonMap("path",path),Object.class,
                sandbox.envdAuthenticationHeaders(opts == null ? null : opts.getUser()), opts == null ? null : opts.getRequestTimeoutSeconds());
    }

    /**
     * Creates a directory.
     *
     * @param path The directory path
     * @throws SandboxException if the operation fails
     */
    public boolean makeDir(String path) throws SandboxException {
        return makeDir(path, null);
    }
    public boolean makeDir(String path, FilesystemRequestOptions opts) throws SandboxException {
        try {
            sandbox.rpc().call("filesystem.Filesystem","MakeDir",java.util.Collections.singletonMap("path",path),Object.class,
                    sandbox.envdAuthenticationHeaders(opts == null ? null : opts.getUser()), opts == null ? null : opts.getRequestTimeoutSeconds());
            return true;
        } catch(ConnectRpcException e) {
            if("already_exists".equals(e.getCode()))return false;
            throw e;
        }
    }
    public EntryInfo stat(String path) throws SandboxException { return stat(path, null); }
    public EntryInfo stat(String path, FilesystemRequestOptions opts) throws SandboxException { StatResponse response=sandbox.rpc().call("filesystem.Filesystem","Stat",java.util.Collections.singletonMap("path",path),StatResponse.class,sandbox.envdAuthenticationHeaders(opts == null ? null : opts.getUser()), opts == null ? null : opts.getRequestTimeoutSeconds());if(response==null||response.entry==null)throw new SandboxException("Expected to receive information about the file or directory");return convert(response.entry); }
    public EntryInfo move(String source,String destination) throws SandboxException { return move(source, destination, null); }
    public EntryInfo move(String source,String destination, FilesystemRequestOptions opts) throws SandboxException { java.util.Map<String,String> p=new java.util.HashMap<>();p.put("source",source);p.put("destination",destination);RawMoveResponse response=sandbox.rpc().call("filesystem.Filesystem","Move",p,RawMoveResponse.class,sandbox.envdAuthenticationHeaders(opts == null ? null : opts.getUser()), opts == null ? null : opts.getRequestTimeoutSeconds());if(response==null||response.entry==null)throw new SandboxException("Expected to receive information about moved object");return convert(response.entry); }
    public EntryInfo rename(String source,String destination) throws SandboxException { return rename(source,destination,null); }
    public EntryInfo rename(String source,String destination, FilesystemRequestOptions opts) throws SandboxException { return move(source,destination,opts); }
    static class StatResponse { RawEntry entry; }
    static class RawMoveResponse { RawEntry entry; }
    static class RawFilesResponse { RawEntry[] entries; }
    static class RawEntry { String name,path,permissions,owner,group,symlinkTarget; long size; String type; int mode; @com.google.gson.annotations.SerializedName(value="modifiedTime",alternate={"modified_time"}) String modifiedTime; }
    /** {@code FileType.FILE_TYPE_DIRECTORY} in {@code filesystem.proto}. */
    private static boolean isDirectory(String type) { return "2".equals(type) || "FILE_TYPE_DIRECTORY".equals(type); }
    private static EntryInfo convert(RawEntry r){ EntryInfo e=new EntryInfo(); if(r==null)return e; e.setName(r.name); e.setPath(r.path); e.setSize(r.size); e.setDir(isDirectory(r.type)); e.setMode(r.mode); e.setPermissions(r.permissions); e.setOwner(r.owner); e.setGroup(r.group); e.setSymlinkTarget(r.symlinkTarget); if(r.modifiedTime!=null)try{e.setModifiedTime(java.time.Instant.parse(r.modifiedTime));}catch(Exception ignored){} return e; }
    public EntryInfo getInfo(String path) throws SandboxException { return getInfo(path, null); }
    public EntryInfo getInfo(String path, FilesystemRequestOptions opts) throws SandboxException { return stat(path, opts); }
    /** Returns false only when the path is absent; other failures propagate. */
    public boolean exists(String path) throws SandboxException { return exists(path, null); }
    public boolean exists(String path, FilesystemRequestOptions opts) throws SandboxException {
        try { stat(path, opts); return true; }
        catch (NotFoundException e) { return false; }
        catch (ConnectRpcException e) { if ("not_found".equals(e.getCode())) return false; throw e; }
    }

    /**
     * Watches a path for filesystem events.
     *
     * <p>The stream is started synchronously so a failure to open it is reported
     * to the caller, and the returned handle is fully wired before any event can
     * arrive (no publication race).
     */
    public WatchHandle watch(String path, boolean recursive, java.util.function.Consumer<FilesystemEvent> consumer) throws SandboxException {
        return watch(path, recursive, consumer, null);
    }
    public WatchHandle watch(String path, boolean recursive, java.util.function.Consumer<FilesystemEvent> consumer, FilesystemRequestOptions opts) throws SandboxException {
        if(recursive&&!sandbox.envdAtLeast("0.1.4"))throw new SandboxException("Recursive filesystem watch requires envd 0.1.4 or later");
        java.util.Map<String,Object> p=new java.util.HashMap<>();p.put("path",path);p.put("recursive",recursive);
        int requestTimeout=opts==null||opts.getRequestTimeoutSeconds()==null?sandbox.getRequestTimeoutSeconds():opts.getRequestTimeoutSeconds();
        final com.google.gson.Gson gson=new com.google.gson.Gson();
        final java.util.concurrent.CompletableFuture<Void> started=new java.util.concurrent.CompletableFuture<>();
        ConnectRpcClient.StreamHandle stream=sandbox.rpc().streamCancellable("filesystem.Filesystem","WatchDir",p,b->{
            com.google.gson.JsonObject o=gson.fromJson(new String(b,java.nio.charset.StandardCharsets.UTF_8),com.google.gson.JsonObject.class);
            // Protobuf JSON writes oneof members at the top level; "event" is
            // the oneof name, not an enclosing wire field. Retain compatibility
            // with older servers/fixtures that supplied that wrapper.
            com.google.gson.JsonObject event=o!=null&&o.has("event")&&o.get("event").isJsonObject()?o.getAsJsonObject("event"):o;
            if(event!=null&&event.has("start")){started.complete(null);return;}
            if(!started.isDone()){started.completeExceptionally(new SandboxException("Expected watch start event"));return;}
            com.google.gson.JsonObject x=event!=null&&event.has("filesystem")?event.getAsJsonObject("filesystem"):null;
            if(x!=null)consumer.accept(gson.fromJson(x,FilesystemEvent.class));
        }, sandbox.envdAuthenticationHeaders(opts == null ? null : opts.getUser()), 0, requestTimeout);
        stream.future().whenComplete((v,t)->{
            if(!started.isDone())started.completeExceptionally(t==null?new SandboxException("Watch stream ended before start event"):t);
        });
        try { if(requestTimeout==0)started.get();else started.get(requestTimeout,java.util.concurrent.TimeUnit.SECONDS); }
        catch(java.util.concurrent.TimeoutException e){stream.cancel();throw new TimeoutException("Timed out waiting for watch start event",e);}
        catch(java.util.concurrent.ExecutionException e){
            stream.cancel();
            Throwable cause=e.getCause();
            if(cause instanceof SandboxException)throw (SandboxException)cause;
            if(cause instanceof java.io.IOException)throw AsyncSupport.transportFailure("Failed to start filesystem watch",(java.io.IOException)cause);
            throw new SandboxException("Failed to start filesystem watch",cause);
        }
        catch(InterruptedException e){Thread.currentThread().interrupt();stream.cancel();throw new SandboxException("Interrupted waiting for watch start event",e);}
        WatchHandle handle=new WatchHandle(stream);
        stream.future().whenComplete((v,t)->{ if(t!=null) handle.setFailure(t); });
        return handle;
    }
    public String createWatcher(String path, boolean recursive) throws SandboxException {return createWatcher(path,recursive,null);}
    public String createWatcher(String path, boolean recursive, FilesystemRequestOptions opts) throws SandboxException {
        if(recursive&&!sandbox.envdAtLeast("0.1.4"))throw new SandboxException("Recursive filesystem watch requires envd 0.1.4 or later");
        java.util.Map<String,Object> p=new java.util.HashMap<>();p.put("path",path);p.put("recursive",recursive);
        WatcherCreateResponse r=sandbox.rpc().call("filesystem.Filesystem","CreateWatcher",p,WatcherCreateResponse.class,sandbox.envdAuthenticationHeaders(opts==null?null:opts.getUser()),opts==null?null:opts.getRequestTimeoutSeconds());
        return r == null ? null : r.watcherId;
    }
    public FilesystemEvent[] getWatcherEvents(String watcherId) throws SandboxException {return getWatcherEvents(watcherId,null);}
    public FilesystemEvent[] getWatcherEvents(String watcherId, FilesystemRequestOptions opts) throws SandboxException {
        WatcherEventsResponse r=sandbox.rpc().call("filesystem.Filesystem","GetWatcherEvents",java.util.Collections.singletonMap("watcherId",watcherId),WatcherEventsResponse.class,sandbox.envdAuthenticationHeaders(opts==null?null:opts.getUser()),opts==null?null:opts.getRequestTimeoutSeconds());
        return r == null || r.events == null ? new FilesystemEvent[0] : r.events;
    }
    public void removeWatcher(String watcherId) throws SandboxException {removeWatcher(watcherId,null);}
    public void removeWatcher(String watcherId, FilesystemRequestOptions opts) throws SandboxException {
        sandbox.rpc().call("filesystem.Filesystem","RemoveWatcher",java.util.Collections.singletonMap("watcherId",watcherId),Object.class,sandbox.envdAuthenticationHeaders(opts==null?null:opts.getUser()),opts==null?null:opts.getRequestTimeoutSeconds());
    }
    public java.util.concurrent.CompletableFuture<String> createWatcherAsync(String path, boolean recursive){return AsyncSupport.supplyAsync(()->{try{return createWatcher(path,recursive);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<FilesystemEvent[]> getWatcherEventsAsync(String watcherId){return AsyncSupport.supplyAsync(()->{try{return getWatcherEvents(watcherId);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<Void> removeWatcherAsync(String watcherId){return AsyncSupport.runAsync(()->{try{removeWatcher(watcherId);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    static class WatcherCreateResponse { @com.google.gson.annotations.SerializedName(value="watcherId",alternate={"watcher_id"}) String watcherId; }
    static class WatcherEventsResponse { FilesystemEvent[] events; }
}
