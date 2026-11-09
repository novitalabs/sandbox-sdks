package ai.novita.sandbox.core;

import java.util.Map;

/**
 * Command execution operations for a sandbox.
 */
public class Commands {
    private final Sandbox sandbox;

    Commands(Sandbox sandbox) {
        this.sandbox = sandbox;
    }

    /**
     * Runs a command and waits for completion.
     *
     * @param command The command to execute
     * @param opts Optional command options
     * @return Result containing stdout, stderr, and exit code
     * @throws SandboxException if execution fails
     */
    public CommandResult run(String command, CommandOpts opts) throws SandboxException {
        // The public JS/Python run API is process.Start + wait; /commands is a
        // legacy control-plane endpoint and is not present on v2 envd clusters.
        ProcessOptions process = new ProcessOptions();
        if (opts != null) {
            process.setWorkingDirectory(opts.getWorkDir());
            process.setEnvironment(opts.getEnv());
            process.setUser(opts.getUser());
            process.setTimeoutSeconds(opts.getTimeout());
            if(opts.getRequestTimeoutSeconds()!=null) process.setRequestTimeoutSeconds(opts.getRequestTimeoutSeconds());
        }
        return runProcess(command, process);
    }

    /**
     * Runs a process through the envd {@code process.v1.Process/Start} streaming
     * RPC and waits for it to finish.
     *
     * @param command The executable to run
     * @param opts Optional process options
     * @return Result containing stdout, stderr, and exit code
     * @throws SandboxException if execution fails
     */
    public CommandResult runProcess(String command, ProcessOptions opts) throws SandboxException {
        return start(command, opts).waitFor();
    }

    public java.util.concurrent.CompletableFuture<CommandResult> runProcessAsync(String command, ProcessOptions opts) { return AsyncSupport.supplyAsync(() -> { try { return runProcess(command,opts); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } }); }
    public java.util.concurrent.CompletableFuture<CommandHandle> startAsync(String command, ProcessOptions opts) { return AsyncSupport.supplyAsync(()->{try{return start(command,opts);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}}); }

    public ProcessInfo[] list() throws SandboxException {
        RawListResponse response=sandbox.rpc().call("process.Process","List",java.util.Collections.emptyMap(),RawListResponse.class);
        if(response==null||response.processes==null)return new ProcessInfo[0];
        ProcessInfo[] result=new ProcessInfo[response.processes.length];
        for(int i=0;i<result.length;i++){
            RawProcess process=response.processes[i];
            RawProcessConfig config=process.config==null?new RawProcessConfig():process.config;
            result[i]=new ProcessInfo(process.pid,process.tag,config.cmd,config.args,config.envs,config.cwd);
        }
        return result;
    }

    static class RawListResponse { RawProcess[] processes; }
    static class RawProcess { int pid; String tag; RawProcessConfig config; }
    static class RawProcessConfig { String cmd,cwd; java.util.List<String> args; java.util.Map<String,String> envs; }

    /**
     * Starts a command without waiting for completion.
     *
     * @param command The command to execute
     * @param opts Optional command options
     * @return Handle to the running command
     * @throws SandboxException if execution fails
     */
    public CommandHandle start(String command, CommandOpts opts) throws SandboxException {
        // Route through the same shell-wrapped StartRequest as run(), so
        // shell syntax and the user option behave identically.
        ProcessOptions process = new ProcessOptions();
        if (opts != null) {
            process.setWorkingDirectory(opts.getWorkDir());
            process.setEnvironment(opts.getEnv());
            process.setUser(opts.getUser());
            process.setTimeoutSeconds(opts.getTimeout());
            if(opts.getRequestTimeoutSeconds()!=null) process.setRequestTimeoutSeconds(opts.getRequestTimeoutSeconds());
        }
        return start(command, process);
    }

    /**
     * Starts a process through the envd streaming RPC without waiting.
     *
     * @param command The executable to run
     * @param opts Optional process options
     * @return Handle to the running process
     * @throws SandboxException if execution fails
     */
    public CommandHandle start(String command, ProcessOptions opts) throws SandboxException {
        if(opts!=null&&Boolean.FALSE.equals(opts.getStdin())&&!sandbox.envdAtLeast("0.3.0"))
            throw new SandboxException("Explicit stdin control requires envd 0.3.0 or later");
        return launch(startRequest(command, opts),opts);
    }

    /** Builds a {@code StartRequest} matching {@code process.v1.StartRequest}. */
    private static Map<String,Object> startRequest(String command, ProcessOptions opts) {
        Map<String,Object> process = new java.util.HashMap<>();
        // JS/Python command helpers invoke a shell consistently, so shell
        // syntax (pipes, redirects, &&) behaves the same across SDKs.
        process.put("cmd", "/bin/bash");
        process.put("args", java.util.Arrays.asList("-l", "-c", command));
        if (opts != null) {
            if (opts.getArgs() != null && !opts.getArgs().isEmpty()) {
                java.util.List<String> args = new java.util.ArrayList<>();
                args.add("-l"); args.add("-c"); args.add(command); args.addAll(opts.getArgs());
                process.put("args", args);
            }
            if (opts.getEnvironment() != null && !opts.getEnvironment().isEmpty()) process.put("envs", opts.getEnvironment());
            if (opts.getWorkingDirectory() != null) process.put("cwd", opts.getWorkingDirectory());
        }
        Map<String,Object> req = new java.util.HashMap<>();
        req.put("process", process);
        if (opts != null) {
            if (opts.getTag() != null) req.put("tag", opts.getTag());
            if (opts.getPty() != null) req.put("pty", java.util.Collections.singletonMap("size", ptySize(opts.getPty())));
            if (opts.getStdin() != null) req.put("stdin", opts.getStdin());
        }
        return req;
    }

    private static Map<String,Object> ptySize(PtySize size) {
        Map<String,Object> s = new java.util.HashMap<>();
        s.put("cols", size.getColumns());
        s.put("rows", size.getRows());
        return s;
    }

    CommandHandle startPty(PtyOptions options) throws SandboxException {
        java.util.Map<String,String> envs=new java.util.HashMap<>(options.getEnvironment());
        envs.putIfAbsent("TERM","xterm-256color");
        envs.putIfAbsent("LANG","C.UTF-8");
        envs.putIfAbsent("LC_ALL","C.UTF-8");
        Map<String,Object> process=new java.util.HashMap<>();
        process.put("cmd","/bin/bash");
        process.put("args",java.util.Arrays.asList("-i","-l"));
        process.put("envs",envs);
        if(options.getWorkingDirectory()!=null)process.put("cwd",options.getWorkingDirectory());
        Map<String,Object> request=new java.util.HashMap<>();
        request.put("process",process);
        request.put("pty",java.util.Collections.singletonMap("size",ptySize(options.getSize())));
        ProcessOptions callbacks=new ProcessOptions().setPty(options.getSize()).setOnPty(options.getOnData()).setUser(options.getUser()).setTimeoutSeconds(options.getTimeoutSeconds());
        if(options.getRequestTimeoutSeconds()!=null)callbacks.setRequestTimeoutSeconds(options.getRequestTimeoutSeconds());
        return launch(request,callbacks);
    }

    /** Starts a stream and wires its lifecycle into a {@link CommandHandle}. */
    private CommandHandle launch(Map<String,Object> req, ProcessOptions opts) throws SandboxException {
        CommandHandle h = new CommandHandle();
        h.setSandbox(sandbox);
        h.setPty(req.containsKey("pty"));
        final StringBuilder out = new StringBuilder(), err = new StringBuilder();
        final int[] exit = {0};
        final boolean[] ended = {false};
        final String[] commandError = {null};
        ConnectRpcClient.StreamHandle sh = sandbox.rpc().streamCancellable("process.Process", "Start", req, b -> {
            ProcessEvent e = ProcessEvent.parse(b);
            if (e.getPid() > 0) h.setPid(e.getPid());
            out.append(e.getPty()).append(e.getStdout());
            h.appendOutput(e.getPty()+e.getStdout(), e.getStderr());
            err.append(e.getStderr());
            if(opts!=null&&!e.getStdout().isEmpty()&&opts.getOnStdout()!=null)opts.getOnStdout().accept(e.getStdout());
            if(opts!=null&&!e.getStderr().isEmpty()&&opts.getOnStderr()!=null)opts.getOnStderr().accept(e.getStderr());
            if(opts!=null&&e.getPtyBytes().length>0&&opts.getOnPty()!=null)opts.getOnPty().accept(e.getPtyBytes());
            if ("end".equals(e.getType())) {
                exit[0] = e.getExitCode(); commandError[0]=e.getError(); ended[0] = true;
                CommandResult result=new CommandResult();result.setStdout(out.toString());result.setStderr(err.toString());result.setExitCode(exit[0]);result.setError(commandError[0]);h.finish(result);
            }
        }, sandbox.envdAuthenticationHeaders(opts == null ? null : opts.getUser()),
                opts == null ? 60 : opts.getTimeoutSeconds(),
                opts == null || opts.getRequestTimeoutSeconds() == null ? sandbox.getRequestTimeoutSeconds() : opts.getRequestTimeoutSeconds());
        h.setStreamHandle(sh);
        sh.future().whenComplete((v, t) -> {
            if (h.getResult() != null) return;
            if (t != null) { h.setFailure(t); return; }
            CommandResult r = new CommandResult();
            r.setStdout(out.toString());
            r.setStderr(err.toString());
            // -1 signals "stream ended without an end event" instead of a fake success.
            r.setExitCode(ended[0] ? exit[0] : -1);
            r.setError(commandError[0]);
            h.setResult(r);
        });
        // JS and Python wait for the first Start event before returning a
        // handle. This guarantees callers can immediately use the real pid for
        // stdin, PTY resize, signal, and reconnect operations.
        try { h.awaitPid(opts == null || opts.getRequestTimeoutSeconds() == null ? sandbox.getRequestTimeoutSeconds() : opts.getRequestTimeoutSeconds()); }
        catch (SandboxException e) { h.disconnect(); throw e; }
        return h;
    }

    public void resize(int pid, PtySize size) throws SandboxException { Map<String,Object> selector=java.util.Collections.singletonMap("pid",pid); Map<String,Object> req=new java.util.HashMap<>();req.put("process",selector);req.put("pty",java.util.Collections.singletonMap("size",ptySize(size)));sandbox.rpc().call("process.Process","Update",req,Object.class); }
    public java.util.concurrent.CompletableFuture<Void> resizeAsync(int pid, PtySize size) { return AsyncSupport.runAsync(() -> { try { resize(pid,size); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } }); }
    public java.util.concurrent.CompletableFuture<Void> sendInputAsync(int pid, byte[] data) { return AsyncSupport.runAsync(() -> { try { sendInput(pid,data); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } }); }

    /** Attaches to an already running process. */
    public CommandHandle connect(int pid) throws SandboxException {
        return connect(pid,(ProcessOptions)null);
    }

    public CommandHandle connect(int pid, ProcessOptions opts) throws SandboxException {
        CommandHandle h = new CommandHandle();
        h.setSandbox(sandbox);

        Map<String,Object> req = new java.util.HashMap<>();
        req.put("process", java.util.Collections.singletonMap("pid", pid));
        final StringBuilder out = new StringBuilder(), err = new StringBuilder();
        final int[] exit = {0};
        final boolean[] ended = {false};
        final String[] commandError = {null};
        ConnectRpcClient.StreamHandle sh = sandbox.rpc().streamCancellable("process.Process", "Connect", req, b -> {
            ProcessEvent e = ProcessEvent.parse(b);
            out.append(e.getPty()).append(e.getStdout());
            h.appendOutput(e.getPty()+e.getStdout(), e.getStderr());
            err.append(e.getStderr());
            if(e.getPid()>0)h.setPid(e.getPid());
            if(opts!=null && opts.getOnPty()!=null && e.getPtyBytes().length>0) opts.getOnPty().accept(e.getPtyBytes());
            if(opts!=null && opts.getOnStdout()!=null && !e.getStdout().isEmpty()) opts.getOnStdout().accept(e.getStdout());
            if(opts!=null && opts.getOnStderr()!=null && !e.getStderr().isEmpty()) opts.getOnStderr().accept(e.getStderr());
            if ("end".equals(e.getType())) {
                exit[0] = e.getExitCode(); commandError[0]=e.getError(); ended[0] = true;
                CommandResult result=new CommandResult();result.setStdout(out.toString());result.setStderr(err.toString());result.setExitCode(exit[0]);result.setError(commandError[0]);h.finish(result);
            }
        }, sandbox.envdAuthenticationHeaders(opts==null?null:opts.getUser()), opts == null ? 60 : opts.getTimeoutSeconds(),
                opts == null || opts.getRequestTimeoutSeconds()==null ? sandbox.getRequestTimeoutSeconds() : opts.getRequestTimeoutSeconds());
        h.setStreamHandle(sh);
        sh.future().whenComplete((v, t) -> {
            if (h.getResult() != null) return;
            if (t != null) { h.setFailure(t); return; }
            CommandResult r = new CommandResult();
            r.setStdout(out.toString());
            r.setStderr(err.toString());
            r.setExitCode(ended[0] ? exit[0] : -1);
            r.setError(commandError[0]);
            h.setResult(r);
        });
        try { h.awaitPid(opts == null || opts.getRequestTimeoutSeconds()==null ? sandbox.getRequestTimeoutSeconds() : opts.getRequestTimeoutSeconds()); }
        catch (SandboxException e) { h.disconnect(); throw e; }
        return h;
    }

    public void sendInput(int pid, byte[] data) throws SandboxException { sendInput(pid, data, false); }
    void sendInput(int pid, byte[] data, boolean pty) throws SandboxException { Map<String,Object> p=new java.util.HashMap<>();p.put("process",java.util.Collections.singletonMap("pid",pid));p.put("input",java.util.Collections.singletonMap(pty ? "pty" : "stdin",java.util.Base64.getEncoder().encodeToString(data)));sandbox.rpc().call("process.Process","SendInput",p,Object.class); }
    public void closeStdin(int pid) throws SandboxException { if(!sandbox.envdAtLeast("0.5.2"))throw new SandboxException("closeStdin requires envd 0.5.2 or later");sandbox.rpc().call("process.Process","CloseStdin",java.util.Collections.singletonMap("process",java.util.Collections.singletonMap("pid",pid)),Object.class); }

    /**
     * Sends a signal to a process.
     *
     * @param pid target process id
     * @param signal either a {@code process.v1.Signal} enum name
     *        ({@code SIGNAL_SIGTERM} / {@code SIGNAL_SIGKILL}) or the short form
     *        ({@code SIGTERM} / {@code SIGKILL})
     * @throws SandboxException if the call fails
     */
    public void sendSignal(int pid, String signal) throws SandboxException {
        Map<String,Object> p = new java.util.HashMap<>();
        p.put("process", java.util.Collections.singletonMap("pid", pid));
        p.put("signal", normalizeSignal(signal));
        sandbox.rpc().call("process.Process", "SendSignal", p, Object.class);
    }

    public boolean kill(int pid) throws SandboxException {
        try { sendSignal(pid,"SIGKILL"); return true; }
        catch(ConnectRpcException e){ if("not_found".equals(e.getCode()))return false; throw e; }
    }

    /** Maps short signal names onto the canonical proto enum names. */
    static String normalizeSignal(String signal) {
        if (signal == null) throw new IllegalArgumentException("signal is required");
        String s = signal.trim().toUpperCase(java.util.Locale.ROOT);
        if (s.startsWith("SIGNAL_")) return s;
        return "SIGNAL_" + s;
    }

    public java.util.concurrent.CompletableFuture<CommandResult> runAsync(String command, CommandOpts opts) { return AsyncSupport.supplyAsync(() -> { try{return run(command,opts);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);} }); }
}
