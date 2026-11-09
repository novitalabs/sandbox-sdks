package ai.novita.sandbox.desktop;

/** noVNC client URL options; password values are encoded as query parameters. */
public class VncUrlOptions {
    private boolean autoConnect=true,viewOnly;
    private String resize="scale",authKey;
    public boolean isAutoConnect(){return autoConnect;}
    public VncUrlOptions setAutoConnect(boolean value){autoConnect=value;return this;}
    public boolean isViewOnly(){return viewOnly;}
    public VncUrlOptions setViewOnly(boolean value){viewOnly=value;return this;}
    public String getResize(){return resize;}
    public VncUrlOptions setResize(String value){if(value!=null&&!java.util.Arrays.asList("off","scale","remote").contains(value))throw new IllegalArgumentException("resize must be off, scale or remote");resize=value;return this;}
    public String getAuthKey(){return authKey;}
    public VncUrlOptions setAuthKey(String value){authKey=value;return this;}
}
