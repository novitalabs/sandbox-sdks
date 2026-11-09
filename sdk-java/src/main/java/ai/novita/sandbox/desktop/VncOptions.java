package ai.novita.sandbox.desktop;

/** Settings for the VNC and noVNC servers. */
public class VncOptions {
    private int vncPort=5900,port=6080,startupTimeoutSeconds=20;
    private boolean requireAuth;
    private String windowId;
    public int getVncPort(){return vncPort;}
    public VncOptions setVncPort(int value){validatePort(value);vncPort=value;return this;}
    public int getPort(){return port;}
    public VncOptions setPort(int value){validatePort(value);port=value;return this;}
    public boolean isRequireAuth(){return requireAuth;}
    public VncOptions setRequireAuth(boolean value){requireAuth=value;return this;}
    public String getWindowId(){return windowId;}
    public VncOptions setWindowId(String value){windowId=value;return this;}
    public int getStartupTimeoutSeconds(){return startupTimeoutSeconds;}
    public VncOptions setStartupTimeoutSeconds(int value){if(value<=0)throw new IllegalArgumentException("startup timeout must be positive");startupTimeoutSeconds=value;return this;}
    private static void validatePort(int value){if(value<1||value>65535)throw new IllegalArgumentException("port must be between 1 and 65535");}
}
