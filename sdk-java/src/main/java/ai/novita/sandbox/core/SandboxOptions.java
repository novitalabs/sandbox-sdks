package ai.novita.sandbox.core;
import java.util.*;
/** Java-style options for control-plane operations. */
public class SandboxOptions extends SandboxOpts {
    public SandboxOptions apiKey(String value){setApiKey(value);return this;}
    public SandboxOptions baseUrl(String value){setBaseUrl(value);return this;}
    public SandboxOptions accessToken(String value){setAccessToken(value);return this;}
    public SandboxOptions sandboxUrl(String value){setSandboxUrl(value);return this;}
    public SandboxOptions debug(boolean value){setDebug(value);return this;}
    public SandboxOptions headers(Map<String,String> value){setHeaders(value);return this;}
    public SandboxOptions requestTimeoutSeconds(int value){setRequestTimeoutSeconds(value);return this;}
    public SandboxOptions logger(java.util.function.Consumer<String> value){setLogger(value);return this;}
    public SandboxOptions domain(String value){setDomain(value);return this;}
    public SandboxOptions metadata(Map<String,String> value){setMetadata(value);return this;}
    public SandboxOptions environment(Map<String,String> value){setEnvVars(value);return this;}
    public SandboxOptions timeoutSeconds(int value){setTimeout(value);return this;}
    public SandboxOptions autoPause(boolean value){setAutoPause(value);return this;}
    public SandboxOptions autoResumeEnabled(boolean value){setAutoResumeEnabled(value);return this;}
    public SandboxOptions secure(boolean value){setSecure(value);return this;}
    public SandboxOptions allowInternetAccess(boolean value){setAllowInternetAccess(value);return this;}
    public SandboxOptions network(NetworkOptions value){setNetwork(value);return this;}
    public SandboxOptions allowPublicTraffic(boolean value){setAllowPublicTraffic(value);return this;}
    public SandboxOptions secretEnvs(Map<String,String> value){setSecretEnvs(value);return this;}
    public SandboxOptions volumeMounts(Map<String,String> value){setVolumeMounts(value);return this;}
    public SandboxOptions mcp(Map<String,Object> value){setMcp(value);return this;}
    public SandboxOptions nodeId(String value){setNodeId(value);return this;}
}
