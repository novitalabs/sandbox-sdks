package ai.novita.sandbox.core;
import java.util.*;

/**
 * Network options, matching the JS/Python {@code SandboxNetworkOpts}: every
 * field is optional and only explicitly set fields are serialized (Gson omits
 * nulls), so a caller-provided object never injects defaults the caller did
 * not choose.
 */
public class NetworkOptions {
    private Boolean allowPublicTraffic;
    private List<String> allowOut;
    private List<String> denyOut;
    private String maskRequestHost;

    public Boolean isAllowPublicTraffic(){return allowPublicTraffic;}
    public NetworkOptions setAllowPublicTraffic(boolean v){allowPublicTraffic=v;return this;}
    public List<String> getAllowOut(){return allowOut;}
    public NetworkOptions setAllowOut(List<String> v){allowOut=v;return this;}
    public List<String> getDenyOut(){return denyOut;}
    public NetworkOptions setDenyOut(List<String> v){denyOut=v;return this;}
    public String getMaskRequestHost(){return maskRequestHost;}
    public NetworkOptions setMaskRequestHost(String v){maskRequestHost=v;return this;}
}
