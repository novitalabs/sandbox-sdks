package ai.novita.sandbox.core; import java.util.*;
public class SecretOptions {
    private String name,value,description;
    private List<String> hosts=new ArrayList<>();
    public SecretOptions name(String v){name=v;return this;}
    public SecretOptions value(String v){value=v;return this;}
    public SecretOptions hosts(List<String> v){hosts=v==null?new ArrayList<>():new ArrayList<>(v);return this;}
    public SecretOptions description(String v){description=v;return this;}
    public String getName(){return name;}
    public String getValue(){return value;}
    public String getDescription(){return description;}
    /** Returns an unmodifiable view so callers cannot mutate the option in place. */
    public List<String> getHosts(){return Collections.unmodifiableList(hosts);}
    Map<String,Object> map(){Map<String,Object> m=new HashMap<>();m.put("name",name);m.put("value",value);m.put("hosts",hosts);m.put("description",description);return m;}
}
