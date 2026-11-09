package ai.novita.sandbox.core;

import java.util.*;
import java.nio.charset.StandardCharsets;

/** Generates ordered build/runtime environment declarations without evaluating data as commands. */
final class TemplateEnvironment {
    static final String PROFILE="/etc/profile.d/99-image-env.sh", ENVIRONMENT="/etc/environment";
    static final String BASE="/etc/.novita-image-env-base.sh", SCRIPT="/tmp/.novita-image-env.sh";
    static final class Group {
        final Map<String,String> values; final boolean runtime;
        Group(Map<String,String> values,boolean runtime){this.values=new LinkedHashMap<>(values);this.runtime=runtime;}
    }
    private static List<String> keys(Map<String,String> values){List<String> keys=new ArrayList<>();for(String key:values.keySet())if(key.matches("[A-Za-z_][A-Za-z0-9_]*"))keys.add(key);return keys;}
    private static final java.util.regex.Pattern REFERENCE=java.util.regex.Pattern.compile("\\$(?:[A-Za-z_][A-Za-z0-9_]*|\\{[A-Za-z_][A-Za-z0-9_]*\\})");
    static String expandable(String value){
        StringBuilder out=new StringBuilder("\"");
        for(int i=0;i<value.length();i++){
            char c=value.charAt(i);
            if(c=='\\'||c=='"'||c=='`'){out.append('\\').append(c);}
            else if(c=='$'){
                java.util.regex.Matcher reference=REFERENCE.matcher(value.substring(i));
                if(reference.lookingAt()){out.append(reference.group());i+=reference.end()-1;}
                else out.append("\\$");
            }else out.append(c);
        }
        return out.append('"').toString();
    }
    static String snapshot(List<Group> groups){
        Map<String,String> names=new LinkedHashMap<>();for(Group group:groups)names.putAll(group.values);
        List<String> keys=keys(names);if(keys.isEmpty())return null;
        List<String> lines=new ArrayList<>(Arrays.asList("set -e","umask 077",": > "+BASE,"command -p chmod 0600 "+BASE));
        for(String name:keys)lines.add(String.format(SNAPSHOT,name,name,BASE,name,BASE,BASE,name,BASE));
        return "command -p sh -c "+Shell.quote(String.join("\n",lines));
    }
    static String command(Map<String,String> literals,List<Group> groups){
        boolean runtime=!keys(literals).isEmpty();for(Group group:groups)runtime|=group.runtime&&!keys(group.values).isEmpty();if(!runtime)return null;
        List<String> lines=new ArrayList<>();lines.add("set -e");
        if(snapshot(groups)!=null){lines.add(". "+Shell.quote(BASE));lines.add("command -p rm -f "+Shell.quote(BASE));}
        lines.add("if [ -f \""+PROFILE+"\" ]; then . \""+PROFILE+"\"; fi");
        lines.add("command -p touch "+PROFILE+" "+ENVIRONMENT);lines.add(FUNCTIONS);
        append(lines,literals,true,true);for(Group group:groups)append(lines,group.values,group.runtime,false);
        lines.add("command -p chmod 0644 "+PROFILE+" "+ENVIRONMENT);
        String encoded=Base64.getEncoder().encodeToString(String.join("\n",lines).getBytes(StandardCharsets.UTF_8));
        return "echo "+encoded+" | command -p base64 -d > "+SCRIPT+" && command -p sh "+SCRIPT+" && command -p rm -f "+SCRIPT;
    }
    private static void append(List<String> lines,Map<String,String> values,boolean runtime,boolean literal){
        List<String> args=new ArrayList<>();for(String key:keys(values)){String value=values.get(key)==null?"":values.get(key);args.add(key);args.add(literal?Shell.quote(value):expandable(value));}
        if(!args.isEmpty())lines.add((runtime?"__novita_runtime_env ":"__novita_build_env ")+String.join(" ",args));
    }
    private static final String FUNCTIONS = "__novita_build_env() {\n  while [ \"$#\" -gt 0 ]; do export \"$1=$2\"; shift 2; done\n}\n__novita_runtime_env() {\n  while [ \"$#\" -gt 0 ]; do\n    export \"$1=$2\"\n    printf \"export %s='\" \"$1\" >> /etc/profile.d/99-image-env.sh\n    printf '%s' \"$2\" | command -p sed \"s/'/'\\\\\\\\''/g\" >> /etc/profile.d/99-image-env.sh\n    printf \"'\\n\" >> /etc/profile.d/99-image-env.sh\n    command -p sed \"/^$1=/d\" /etc/environment > /etc/environment.tmp\n    command -p cat /etc/environment.tmp > /etc/environment\n    command -p rm -f /etc/environment.tmp\n    case \"$2\" in\n      *'\"'*) ;;\n      *) if [ \"$(printf '%s' \"$2\" | command -p wc -l)\" -eq 0 ]; then printf '%s=%s\\n' \"$1\" \"$2\" >> /etc/environment; fi ;;\n    esac\n    shift 2\n  done\n}";
    private static final String SNAPSHOT = "if [ \"${%s+x}\" = x ]; then\n  printf \"export %s='\" >> %s\n  printf '%%s' \"${%s}\" | command -p sed \"s/'/'\\\\\\\\''/g\" >> %s\n  printf \"'\\n\" >> %s\nelse\n  printf 'unset %s\\n' >> %s\nfi";
}
