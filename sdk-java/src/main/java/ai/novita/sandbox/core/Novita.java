package ai.novita.sandbox.core;

import java.util.concurrent.CompletableFuture;

/** Root facade for the Novita Sandbox Java SDK. */
public final class Novita implements AutoCloseable {
    private final SandboxOpts options;
    /** Single shared control-plane client for templates, volumes and secrets. */
    private final ApiClient client;
    private final Template templates;
    private final Volume volumes;
    private final Secret secrets;
    private final Job jobs;
    private volatile boolean closed;

    private Novita(SandboxOpts options) throws SandboxException {
        this.options = options.copy();
        this.client = new ApiClient(this.options);
        this.templates = new Template(client);
        this.volumes = new Volume(client);
        this.secrets = new Secret(client);
        this.jobs = new Job(client);
    }

    public static Novita create() throws SandboxException { return new Novita(new SandboxOptions()); }

    public static Novita create(SandboxOptions options) throws SandboxException { return new Novita(options == null ? new SandboxOptions() : options); }

    public static Novita create(SandboxOpts options) throws SandboxException { return new Novita(options == null ? new SandboxOpts() : options); }

    public Template templates() { return templates; }
    public Job job() { return jobs; }
    public Job jobs() { return jobs; }
    public Volume volumes(){return volumes;} public Secret secrets(){return secrets;}
    public Template template(){return templates;} public Volume volume(){return volumes;} public Secret secret(){return secrets;}
    public Sandbox createSandbox(String template) throws SandboxException { return Sandbox.create(template, options); }
    public Sandbox connectSandbox(String id) throws SandboxException { return Sandbox.connect(id, options); }
    public SandboxInfo[] listSandboxes() throws SandboxException { return client.listSandboxes(); }
    /** Fetches one page using this facade's connection configuration. */
    public SandboxListResult listSandboxes(SandboxListOptions options) throws SandboxException { return client.listSandboxesPage(options); }
    /** Traverses every remaining page without mutating the supplied filters. */
    public SandboxInfo[] listAllSandboxes(SandboxListOptions options) throws SandboxException { return client.listAllSandboxes(options); }
    public SandboxInfo[] listAllSandboxes() throws SandboxException { return listAllSandboxes(null); }
    public CompletableFuture<SandboxListResult> listSandboxesAsync(SandboxListOptions options) { return AsyncSupport.supplyAsync(() -> { try{return listSandboxes(options);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);} }); }
    public CompletableFuture<SandboxInfo[]> listAllSandboxesAsync(SandboxListOptions options) { return AsyncSupport.supplyAsync(() -> { try{return listAllSandboxes(options);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);} }); }
    public CompletableFuture<Sandbox> createSandboxAsync(String template) { return AsyncSupport.supplyAsync(() -> { try{return createSandbox(template);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);} }); }
    public CompletableFuture<Sandbox> connectSandboxAsync(String id) { return AsyncSupport.supplyAsync(() -> { try{return connectSandbox(id);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);} }); }
    public CompletableFuture<SandboxInfo[]> listSandboxesAsync() { return AsyncSupport.supplyAsync(() -> { try{return listSandboxes();}catch(Exception e){throw new java.util.concurrent.CompletionException(e);} }); }

    /** Releases the shared control-plane HTTP client. Sandboxes created by this facade own their own clients. */
    @Override public void close() {
        if (closed) return;
        closed = true;
        client.close();
    }
}
