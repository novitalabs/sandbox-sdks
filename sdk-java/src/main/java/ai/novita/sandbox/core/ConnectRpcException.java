package ai.novita.sandbox.core;

/** Error returned in a Connect protocol trailer frame. */
public final class ConnectRpcException extends SandboxException {
    private final String code;
    private final Integer httpStatus;
    public ConnectRpcException(String code, String message, Integer httpStatus) {
        super(message == null ? "Connect RPC error" : message);
        this.code = code;
        this.httpStatus = httpStatus;
    }
    public String getCode() { return code; }
    public Integer getHttpStatus() { return httpStatus; }
}
