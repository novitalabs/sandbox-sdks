package ai.novita.sandbox.core;
/** Result metadata for a filesystem write. */
public final class WriteInfo { private String name,path,type; private long size; private int mode; public String getName(){return name;} public String getPath(){return path;} public String getType(){return type;} public long getSize(){return size;} public int getMode(){return mode;} void setName(String v){name=v;} void setPath(String v){path=v;} void setType(String v){type=v;} void setSize(long v){size=v;} void setMode(int v){mode=v;} }
