package ai.novita.sandbox.core;

class ApiExceptions {
    static SandboxException from(int code, String message) {
        String m = "API error " + code + ": " + message;
        if (code == 401) return new AuthenticationException(m);
        if (code == 403) return new ForbiddenException(m);
        if (code == 404) return new NotFoundException(m);
        if (code == 408 || code == 504) return new TimeoutException(m);
        if (code == 429) return new RateLimitException(m);
        return new SandboxException(m);
    }

    /**
     * True when {@code e} was produced by {@link #from} for the given HTTP status.
     *
     * <p>Used for statuses that have no dedicated exception type (e.g. 415).
     * Matching the {@code "API error <code>: "} prefix is exact, unlike a bare
     * {@code contains("415")} which also matches a body that merely mentions it.
     */
    static boolean isStatus(SandboxException e, int code) {
        String m = e.getMessage();
        return m != null && m.startsWith("API error " + code + ": ");
    }
}
