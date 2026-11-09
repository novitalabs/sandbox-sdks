package ai.novita.sandbox.core;

/** A strict image configuration read failed before a platform build was submitted. */
public final class ImageConfigException extends RuntimeException {
    public ImageConfigException(String message) { super(message); }
}
