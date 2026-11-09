package ai.novita.sandbox.desktop;

import ai.novita.sandbox.core.CommandHandle;
import ai.novita.sandbox.core.CommandOpts;
import ai.novita.sandbox.core.CommandResult;
import ai.novita.sandbox.core.Sandbox;
import ai.novita.sandbox.core.SandboxException;
import ai.novita.sandbox.core.SandboxOpts;
import ai.novita.sandbox.core.Shell;
import ai.novita.sandbox.core.AsyncSupport;

/**
 * Desktop sandbox with GUI automation capabilities.
 */
public class DesktopSandbox implements AutoCloseable {
    private final Sandbox sandbox;
    private final VncServer stream;
    private volatile boolean closed;
    private volatile CommandHandle displayHandle;
    private final java.util.List<CommandHandle> launched=new java.util.concurrent.CopyOnWriteArrayList<>();
    private String display = ":0";
    private int width=1024,height=768,dpi=96;

    /**
     * Creates a new desktop sandbox.
     *
     * @param template The template to use
     * @param opts Optional configuration
     * @return A new DesktopSandbox instance
     * @throws SandboxException if creation fails
     */
    public static DesktopSandbox create(String template, SandboxOpts opts) throws SandboxException {
        DesktopOptions desktopOptions=opts instanceof DesktopOptions?(DesktopOptions)opts:new DesktopOptions();
        SandboxOpts effective=opts==null?new SandboxOpts():opts.copy();
        java.util.Map<String,String> env=new java.util.LinkedHashMap<>();if(effective.getEnvVars()!=null)env.putAll(effective.getEnvVars());
        env.put("DISPLAY",desktopOptions.getDisplay());effective.setEnvVars(env);
        Sandbox sandbox=Sandbox.create(template,effective);
        DesktopSandbox desktop=wrap(sandbox,desktopOptions);
        try { desktop.initializeDisplay(); return desktop; }
        catch(SandboxException|RuntimeException error){try{sandbox.kill();}catch(SandboxException cleanup){error.addSuppressed(cleanup);}throw error;}
    }
    private DesktopSandbox(Sandbox sandbox) {
        this.sandbox=sandbox;
        this.stream=new VncServer(this);
    }
    public static DesktopSandbox wrap(Sandbox sandbox){return wrap(sandbox,new DesktopOptions());}
    public static DesktopSandbox wrap(Sandbox sandbox,DesktopOptions options){
        DesktopSandbox desktop=new DesktopSandbox(java.util.Objects.requireNonNull(sandbox));
        if(options!=null){desktop.display=options.getDisplay();desktop.width=options.getWidth();desktop.height=options.getHeight();desktop.dpi=options.getDpi();}
        return desktop;
    }
    /** Connects to the existing display; it does not start a second X server. */
    public static DesktopSandbox connect(String id,SandboxOpts opts)throws SandboxException{
        return wrap(Sandbox.connect(id,opts),opts instanceof DesktopOptions?(DesktopOptions)opts:new DesktopOptions());
    }
    public static DesktopSandbox connect(String id)throws SandboxException{return connect(id,null);}
    public String getDisplay(){return display;}
    public int getWidth(){return width;}
    public int getHeight(){return height;}
    public int getDpi(){return dpi;}

    private void initializeDisplay() throws SandboxException {
        ai.novita.sandbox.core.ProcessOptions opts = new ai.novita.sandbox.core.ProcessOptions();
        java.util.Map<String,String> env = new java.util.HashMap<>();
        env.put("DISPLAY", display);
        opts.setEnvironment(env).setTimeoutSeconds(0);
        CommandHandle xvfb = sandbox.commands().start(
                "Xvfb " + display + " -ac -screen 0 " + width + "x" + height + "x24 -retro -dpi " + dpi + " -nolisten tcp -nolisten unix", opts);
        displayHandle = xvfb;
        long deadline = System.nanoTime() + java.util.concurrent.TimeUnit.SECONDS.toNanos(20);
        while (System.nanoTime() < deadline) {
            try {
                CommandResult result = run("xdpyinfo -display " + display);
                if (result.getExitCode() == 0) { launch("startxfce4"); return; }
            } catch (SandboxException ignored) { }
            try { Thread.sleep(500); } catch (InterruptedException e) { Thread.currentThread().interrupt(); throw new SandboxException("interrupted starting desktop display", e); }
        }
        throw new SandboxException("could not start Xvfb display " + display);
    }

    /**
     * Gets the underlying core sandbox.
     */
    public Sandbox getSandbox() {
        return sandbox;
    }
    public VncServer getStream() { return stream; }

    /**
     * Performs a mouse click at coordinates.
     *
     * @param x X coordinate
     * @param y Y coordinate
     * @param opts Optional click options; {@code button} selects the mouse button
     *        and {@code clicks} the repeat count
     * @throws SandboxException if the operation fails
     */
    public void click(int x, int y, ClickOptions opts) throws SandboxException {
        String button = opts == null || opts.getButton() == null ? "left" : opts.getButton();
        int clicks = opts == null ? 1 : Math.max(1, opts.getClicks());
        StringBuilder cmd = new StringBuilder("xdotool mousemove --sync ").append(x).append(' ').append(y).append(" click");
        if (clicks > 1) cmd.append(" --repeat ").append(clicks);
        cmd.append(' ').append(Shell.quote(buttonCode(button)));
        run(cmd.toString());
    }

    /**
     * Types text via keyboard.
     *
     * @param text The text to type
     * @throws SandboxException if the operation fails
     */
    public void type(String text) throws SandboxException {
        run("xdotool type -- " + Shell.quote(text));
    }
    public void leftClick(int x,int y) throws SandboxException { click(x,y,null); }
    public void doubleClick(int x,int y) throws SandboxException { run("xdotool mousemove --sync "+x+" "+y+" click --repeat 2 1"); }
    public void rightClick(int x,int y) throws SandboxException { run("xdotool mousemove --sync "+x+" "+y+" click 3"); }
    public void middleClick(int x,int y) throws SandboxException { run("xdotool mousemove --sync "+x+" "+y+" click 2"); }
    public void scroll(int clicks) throws SandboxException { if(clicks==0)return; run("xdotool click --repeat "+Math.abs(clicks)+" "+(clicks>0?"4":"5")); }
    public void mouseDown(String button) throws SandboxException { run("xdotool mousedown "+Shell.quote(buttonCode(button))); }
    public void mouseUp(String button) throws SandboxException { run("xdotool mouseup "+Shell.quote(buttonCode(button))); }
    public void write(String text) throws SandboxException { type(text); }
    public void drag(int fromX,int fromY,int toX,int toY) throws SandboxException { moveMouse(fromX,fromY); mouseDown("left"); run("xdotool mousemove --sync "+toX+" "+toY); mouseUp("left"); }
    public void waitSeconds(double seconds) throws SandboxException { if(seconds<0)throw new SandboxException("seconds must be non-negative"); try{Thread.sleep((long)(seconds*1000));}catch(InterruptedException e){Thread.currentThread().interrupt();throw new SandboxException("wait interrupted",e);} }
    public int[] cursorPosition() throws SandboxException { CommandResult r=run("xdotool getmouselocation --shell");int x=parseVar(r.getStdout(),"X");int y=parseVar(r.getStdout(),"Y");if(x<0||y<0)throw new SandboxException("unable to parse cursor position from: "+r.getStdout());return new int[]{x,y}; }
    public int[] screenSize() throws SandboxException { CommandResult r=run("xdpyinfo | awk '/dimensions:/{print $2}'");String[] p=r.getStdout().trim().split("x");if(p.length!=2)throw new SandboxException("unable to determine screen size from: "+r.getStdout());return new int[]{Integer.parseInt(p[0]),Integer.parseInt(p[1])}; }
    public void moveMouse(int x,int y) throws SandboxException { run("xdotool mousemove --sync "+x+" "+y); }
    public void press(String key) throws SandboxException { run("xdotool key "+Shell.quote(mapKey(key))); }
    public void open(String target) throws SandboxException { launch("xdg-open "+Shell.quote(target)); }

    /**
     * Launches a command in the background.
     *
     * <p>The returned handle is retained so {@link #kill()} can terminate it.
     *
     * @param command the command to launch
     * @throws SandboxException if launching fails
     */
    public void launch(String command) throws SandboxException {
        CommandOpts options = commandOptions();options.setTimeout(0);
        launched.add(sandbox.commands().start(command, options));
    }
    public void launchApplication(String application,String uri)throws SandboxException{launch("gtk-launch "+Shell.quote(application)+(uri==null?"":" "+Shell.quote(uri)));}
    public void press(String... keys)throws SandboxException{String[] mapped=java.util.Arrays.stream(keys).map(DesktopSandbox::mapKey).toArray(String[]::new);run("xdotool key "+Shell.quote(String.join("+",mapped)));}
    public String[] windowIds()throws SandboxException{String value=run("xdotool search --name '.*'").getStdout().trim();return value.isEmpty()?new String[0]:value.split("\\r?\\n");}
    public String getWindowTitle(String id)throws SandboxException{return run("xdotool getwindowname "+Shell.quote(id)).getStdout().trim();}
    public String getCurrentWindowId()throws SandboxException{return run("xdotool getwindowfocus").getStdout().trim();}
    public String getWindowId(String title)throws SandboxException{return run("xdotool search --name "+Shell.quote(title)).getStdout().trim();}
    public void focusWindow(String id)throws SandboxException{run("xdotool windowactivate --sync "+Shell.quote(id));}
    public void closeWindow(String id)throws SandboxException{run("xdotool windowclose "+Shell.quote(id));}
    public void moveWindow(String id,int x,int y)throws SandboxException{run("xdotool windowmove "+Shell.quote(id)+" "+x+" "+y);}
    public void resizeWindow(String id,int width,int height)throws SandboxException{if(width<=0||height<=0)throw new IllegalArgumentException("Window size must be positive");run("xdotool windowsize "+Shell.quote(id)+" "+width+" "+height);}
    public String activeWindowTitle() throws SandboxException { return run("xdotool getactivewindow getwindowname").getStdout().trim(); }
    public String[] windows() throws SandboxException { String s=run("xdotool search --name '.*' getwindowname %@").getStdout().trim(); return s.isEmpty()?new String[0]:s.split("\\r?\\n"); }
    public java.util.concurrent.CompletableFuture<Void> launchAsync(String command){return AsyncSupport.runAsync(()->{try{launch(command);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}

    /**
     * Takes a screenshot.
     *
     * @return Screenshot data as PNG bytes
     * @throws SandboxException if the operation fails
     */
    public byte[] screenshot() throws SandboxException {return screenshot("png");}
    public byte[] screenshot(String format) throws SandboxException {
        if(!"png".equalsIgnoreCase(format)&&!"jpeg".equalsIgnoreCase(format)&&!"jpg".equalsIgnoreCase(format))throw new IllegalArgumentException("Screenshot format must be png or jpeg");
        // Unique path per call so concurrent screenshots cannot clobber each other.
        String path = "/tmp/novita-screenshot-" + java.util.UUID.randomUUID() + ("png".equalsIgnoreCase(format)?".png":".jpg");
        try {
            // JS/Python desktop runtimes provide scrot; gnome-screenshot is
            // not installed in the standard desktop template.
            run("scrot --pointer " + Shell.quote(path));
            return sandbox.files().read(path);
        } finally {
            try { sandbox.files().remove(path); } catch (SandboxException ignored) { /* best effort cleanup */ }
        }
    }

    /**
     * Terminates the sandbox.
     *
     * @throws SandboxException if termination fails
     */
    public void kill() throws SandboxException {
        if (closed) return;
        synchronized (this) {
            if (closed) return;
            try {
                for(CommandHandle handle:launched)handle.disconnect();
                if(displayHandle!=null)displayHandle.disconnect();
                stream.stop();
            } finally {
                sandbox.kill();
                closed = true;
            }
        }
    }
    public java.util.concurrent.CompletableFuture<Void> killAsync(){return AsyncSupport.runAsync(()->{try{kill();}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    @Override public void close() throws SandboxException { kill(); }
    public java.util.concurrent.CompletableFuture<Void> clickAsync(int x,int y,ClickOptions opts){return AsyncSupport.runAsync(()->{try{click(x,y,opts);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<Void> typeAsync(String text){return AsyncSupport.runAsync(()->{try{type(text);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<Void> pressAsync(String key){return AsyncSupport.runAsync(()->{try{press(key);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<Void> moveMouseAsync(int x,int y){return AsyncSupport.runAsync(()->{try{moveMouse(x,y);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<Void> openAsync(String target){return AsyncSupport.runAsync(()->{try{open(target);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<Void> waitSecondsAsync(double seconds){return AsyncSupport.runAsync(()->{try{waitSeconds(seconds);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<Void> dragAsync(int fromX,int fromY,int toX,int toY){return AsyncSupport.runAsync(()->{try{drag(fromX,fromY,toX,toY);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<int[]> cursorPositionAsync(){return AsyncSupport.supplyAsync(()->{try{return cursorPosition();}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<int[]> screenSizeAsync(){return AsyncSupport.supplyAsync(()->{try{return screenSize();}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<byte[]> screenshotAsync(){return AsyncSupport.supplyAsync(()->{try{return screenshot();}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}

    /** Runs a command and fails loudly when it exits non-zero. */
    private CommandResult run(String command) throws SandboxException {
        CommandResult r = sandbox.commands().run(command, commandOptions());
        if (r != null && r.getExitCode() != 0) {
            String detail = r.getStderr() == null || r.getStderr().trim().isEmpty() ? r.getStdout() : r.getStderr();
            throw new SandboxException("command failed (exit " + r.getExitCode() + "): " + command + (detail == null ? "" : "\n" + detail.trim()));
        }
        return r;
    }

    private CommandOpts commandOptions(){CommandOpts opts=new CommandOpts();opts.setEnv(java.util.Collections.singletonMap("DISPLAY",display));return opts;}
    private static String mapKey(String key){String k=key.toLowerCase(java.util.Locale.ROOT);java.util.Map<String,String> m=new java.util.HashMap<>();m.put("enter","Return");m.put("esc","Escape");m.put("escape","Escape");m.put("ctrl","Control_L");m.put("control","Control_L");m.put("alt","Alt_L");m.put("shift","Shift_L");m.put("backspace","BackSpace");m.put("delete","Delete");m.put("tab","Tab");m.put("space","space");m.put("up","Up");m.put("down","Down");m.put("left","Left");m.put("right","Right");m.put("home","Home");m.put("end","End");return m.containsKey(k)?m.get(k):key;}

    public static final class VncServer {
        private final Sandbox sandbox; private final DesktopSandbox desktop; private CommandHandle handle; private int vncPort=5900, port=6080; private String authKey; private boolean running;
        VncServer(DesktopSandbox desktop){this.desktop=desktop;this.sandbox=desktop.sandbox;}
        public synchronized void start() throws SandboxException { start(vncPort,port,false,null); }
        public synchronized void start(int vncPort,int port,boolean requireAuth,String windowId) throws SandboxException {
            start(new VncOptions().setVncPort(vncPort).setPort(port).setRequireAuth(requireAuth).setWindowId(windowId));
        }
        public synchronized void start(VncOptions options) throws SandboxException {
            VncOptions effective=options==null?new VncOptions():options;
            int vncPort=effective.getVncPort(),port=effective.getPort();boolean requireAuth=effective.isRequireAuth();String windowId=effective.getWindowId();
            if(running)throw new SandboxException("VNC server is already running");
            this.vncPort=vncPort;this.port=port;
            authKey=null;
            String auth="-nopw";
            if(requireAuth){
                authKey=java.util.UUID.randomUUID().toString().replace("-","").substring(0,16);
                sandbox.commands().run("mkdir -p ~/.vnc && x11vnc -storepasswd "+Shell.quote(authKey)+" ~/.vnc/passwd",null);
                auth="-usepw";
            }
            String id=windowId==null?"":" -id "+Shell.quote(windowId);
            sandbox.commands().run("x11vnc -bg -display "+desktop.display+" -forever -shared -rfbport "+vncPort+" "+auth+id,null);
            CommandOpts processOptions=desktop.commandOptions();processOptions.setTimeout(0);
            running=true;
            try {
                handle=sandbox.commands().start("cd /opt/noVNC/utils && ./novnc_proxy --vnc localhost:"+vncPort+" --listen "+port+" --web /opt/noVNC",processOptions);
                String probe="until netstat -tuln | grep -q "+Shell.quote(":"+port+" ")+"; do sleep 0.1; done";
                CommandOpts readiness=desktop.commandOptions();readiness.setTimeout(effective.getStartupTimeoutSeconds()+1);
                sandbox.commands().run("timeout "+effective.getStartupTimeoutSeconds()+" sh -c "+Shell.quote(probe),readiness);
            } catch(SandboxException failure){try{stop();}catch(SandboxException cleanup){failure.addSuppressed(cleanup);}throw failure;}
        }
        public synchronized void stop() throws SandboxException {
            if(!running)return;
            // Match only the x11vnc instance bound to this server's RFB port.
            try { sandbox.commands().run("pkill -f "+Shell.quote("x11vnc .*-rfbport "+vncPort),null); }
            catch(ai.novita.sandbox.core.CommandExitException e){if(e.getResult().getExitCode()!=1)throw e;}
            finally { try{if(handle!=null)handle.kill();}finally{handle=null;running=false;authKey=null;} }
        }
        public synchronized boolean isRunning(){return running;}
        public synchronized String getAuthKey(){if(authKey==null)throw new IllegalStateException("VNC authentication is disabled");return authKey;}
        public synchronized String getUrl() throws SandboxException {return getUrl(null);}
        public synchronized String getUrl(VncUrlOptions options) throws SandboxException {
            VncUrlOptions effective=options==null?new VncUrlOptions():options;
            okhttp3.HttpUrl.Builder url=okhttp3.HttpUrl.get(sandbox.portUrl(port)+"/vnc.html").newBuilder();
            if(effective.isAutoConnect())url.addQueryParameter("autoconnect","true");
            if(effective.isViewOnly())url.addQueryParameter("view_only","true");
            if(effective.getResize()!=null)url.addQueryParameter("resize",effective.getResize());
            if(effective.getAuthKey()!=null)url.addQueryParameter("password",effective.getAuthKey());
            return url.build().toString();
        }
    }

    private static String buttonCode(String button){String b=button.toLowerCase();if("left".equals(b))return "1";if("middle".equals(b))return "2";if("right".equals(b))return "3";return button;}
    private static int parseVar(String s,String key){for(String line:s.split("\\n")){if(line.startsWith(key+"="))return Integer.parseInt(line.substring(key.length()+1).trim());}return -1;}
}
