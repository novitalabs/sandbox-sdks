package ai.novita.sandbox.desktop;

import ai.novita.sandbox.core.SandboxOpts;

/** Sandbox creation/connection options plus the desktop's X11 configuration. */
public class DesktopOptions extends SandboxOpts {
    private String display=":0";
    private int width=1024,height=768,dpi=96;
    public String getDisplay(){return display;}
    public DesktopOptions setDisplay(String value){if(value==null||!value.matches(":\\d+(?:\\.\\d+)?"))throw new IllegalArgumentException("Invalid X11 display");display=value;return this;}
    public int getWidth(){return width;}
    public int getHeight(){return height;}
    public DesktopOptions setResolution(int width,int height){if(width<=0||height<=0)throw new IllegalArgumentException("Resolution must be positive");this.width=width;this.height=height;return this;}
    public int getDpi(){return dpi;}
    public DesktopOptions setDpi(int value){if(value<=0)throw new IllegalArgumentException("DPI must be positive");dpi=value;return this;}
}
