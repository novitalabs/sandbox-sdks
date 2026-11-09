package ai.novita.sandbox.core;
/** A file path and bytes to write to a sandbox. */
public final class WriteEntry { private final String path; private final byte[] data; public WriteEntry(String path,byte[] data){if(path==null||path.isEmpty())throw new IllegalArgumentException("path is required");this.path=path;this.data=data==null?new byte[0]:data.clone();} public String getPath(){return path;} public byte[] getData(){return data.clone();} }
