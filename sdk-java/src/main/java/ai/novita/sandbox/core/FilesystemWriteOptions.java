package ai.novita.sandbox.core;
public class FilesystemWriteOptions {
    private Integer requestTimeoutSeconds;
    public Integer getRequestTimeoutSeconds(){return requestTimeoutSeconds;}
    public FilesystemWriteOptions setRequestTimeoutSeconds(int value){if(value<0)throw new IllegalArgumentException("request timeout must be non-negative");requestTimeoutSeconds=value;return this;}
 private int mode=0644; private boolean gzip, useOctetStream; private String username,version; public int getMode(){return mode;} public FilesystemWriteOptions setMode(int mode){this.mode=mode;return this;} public boolean isGzip(){return gzip;} public FilesystemWriteOptions setGzip(boolean gzip){this.gzip=gzip;return this;} public boolean isUseOctetStream(){return useOctetStream;} public FilesystemWriteOptions setUseOctetStream(boolean value){useOctetStream=value;return this;} public String getUsername(){return username;} public FilesystemWriteOptions setUsername(String value){username=value;return this;} public String getVersion(){return version;} public FilesystemWriteOptions setVersion(String value){version=value;return this;} }
