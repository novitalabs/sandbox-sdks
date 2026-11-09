package ai.novita.sandbox.core;
/** Options for binary filesystem reads. */
public class FilesystemReadOptions {
    private Integer requestTimeoutSeconds;
    public Integer getRequestTimeoutSeconds(){return requestTimeoutSeconds;}
    public FilesystemReadOptions setRequestTimeoutSeconds(int value){if(value<0)throw new IllegalArgumentException("request timeout must be non-negative");requestTimeoutSeconds=value;return this;}
 private boolean gzip; private String username,version; public boolean isGzip(){return gzip;} public FilesystemReadOptions setGzip(boolean value){gzip=value;return this;} public String getUsername(){return username;} public FilesystemReadOptions setUsername(String value){username=value;return this;} public String getVersion(){return version;} public FilesystemReadOptions setVersion(String value){version=value;return this;} }
