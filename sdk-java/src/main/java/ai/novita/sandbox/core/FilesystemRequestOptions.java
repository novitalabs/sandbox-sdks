package ai.novita.sandbox.core;

/** Common options for filesystem RPC operations. */
public class FilesystemRequestOptions {
    private Integer requestTimeoutSeconds;
    public Integer getRequestTimeoutSeconds(){return requestTimeoutSeconds;}
    public FilesystemRequestOptions setRequestTimeoutSeconds(int value){if(value<0)throw new IllegalArgumentException("request timeout must be non-negative");requestTimeoutSeconds=value;return this;}

    private String user;

    public String getUser() {
        return user;
    }

    public FilesystemRequestOptions setUser(String value) {
        user = value;
        return this;
    }
}
