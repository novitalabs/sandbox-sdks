package ai.novita.sandbox.core;

import com.google.gson.*;
import okhttp3.mockwebserver.*;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.util.*;
import static org.junit.jupiter.api.Assertions.*;

class TemplateEnvironmentTest {
    @TempDir Path dir;
    String rewrite(String value){return value.replace("/etc/profile.d/99-image-env.sh",dir.resolve("profile").toString()).replace("/etc/environment",dir.resolve("environment").toString()).replace("/etc/.novita-image-env-base.sh",dir.resolve("base").toString()).replace("/tmp/.novita-image-env.sh",dir.resolve("script").toString());}
    String run(String command,Map<String,String> env)throws Exception{
        ProcessBuilder pb=new ProcessBuilder("/bin/sh","-c",command).redirectErrorStream(true);pb.environment().clear();pb.environment().putAll(env);
        Process p=pb.start();String output=new String(p.getInputStream().readAllBytes(),StandardCharsets.UTF_8);assertEquals(0,p.waitFor(),output);return output;
    }
    List<String> environment(TemplateBuilder builder,String... names)throws Exception{
        Files.writeString(dir.resolve("profile"),"export BASE='kept'\n");Files.writeString(dir.resolve("environment"),"BASE=kept\n");
        JsonObject spec=JsonParser.parseString(builder.toJson()).getAsJsonObject();
        Map<String,String> env=new HashMap<>();env.put("PATH","/usr/bin:/bin");env.put("LATER","base");env.put("A","base");
        for(JsonElement element:spec.getAsJsonArray("steps")){
            JsonObject step=element.getAsJsonObject();JsonArray args=step.getAsJsonArray("args");String type=step.get("type").getAsString();
            if("ENV".equals(type)){for(int i=0;i+1<args.size();i+=2)env.put(args.get(i).getAsString(),args.get(i+1).getAsString());continue;}
            if(!"RUN".equals(type)||args==null||args.size()==0||!args.get(0).getAsString().contains("image-env"))continue;
            String command=args.get(0).getAsString();
            if(command.startsWith("echo ")){String[] parts=command.split(" ",3);String decoded=new String(Base64.getDecoder().decode(parts[1].replace("'","")),StandardCharsets.UTF_8);parts[1]=Base64.getEncoder().encodeToString(rewrite(decoded).getBytes(StandardCharsets.UTF_8));command=String.join(" ",parts);}
            run(rewrite(command),env);
        }
        StringBuilder query=new StringBuilder(". "+Shell.quote(dir.resolve("profile").toString())+"; ");for(String name:names)query.append("printf '%s\\000' \"${").append(name).append("-}\"; ");
        String[] values=run(query.toString(),Collections.singletonMap("PATH","/different")).split("\u0000",-1);return Arrays.asList(values).subList(0,names.length);
    }
    @Test void buildOnlyAndDockerfileDeclarationGroups()throws Exception{
        TemplateBuilder b=TemplateBuilder.create().fromDockerfile("FROM image\nARG ROOT=/first\nENV SAVE=$ROOT\nARG ROOT=/second\nENV A=first\nENV A=second B=$A\nENV PRICE=\"hello B=world\" WORDS=\"two words\"\n").inheritImageConfig(false);
        assertEquals(Arrays.asList("/first","","second","first","hello B=world","two words","kept"),environment(b,"SAVE","ROOT","A","B","PRICE","WORDS","BASE"));
        assertEquals(Collections.singletonList(""),environment(TemplateBuilder.create().fromImage("image",false).setEnvs(Collections.singletonMap("TOKEN","secret")),"TOKEN"));
    }
    @Test void runtimePreservesOrderLiteralValuesAndBaseFiles()throws Exception{
        try(MockWebServer server=new MockWebServer()){
            server.start();server.enqueue(new MockResponse().setBody("{\"config\":{\"digest\":\"sha256:cfg\"}}"));server.enqueue(new MockResponse().setBody("{\"config\":{\"Env\":[\"PATH=/image:/usr/bin:/bin\",\"PRICE=cost$5\"]}}"));
            TemplateBuilder b=TemplateBuilder.create().fromImage(server.getHostName()+":"+server.getPort()+"/image")
                .setEnvs(Collections.singletonMap("PATH","/one:$PATH"),true).setEnvs(Collections.singletonMap("PATH","/two:$PATH"),true)
                .setEnvs(Collections.singletonMap("SAVE","$LATER"),true).setEnvs(Collections.singletonMap("LATER","build-secret"))
                .setEnvs(Collections.singletonMap("v","quoted'\n\n"),true).setEnvs(Collections.singletonMap("EMPTY",""),true);
            assertEquals(Arrays.asList("/two:/one:/image:/usr/bin:/bin","base","","quoted'\n\n","","cost$5","kept"),environment(b,"PATH","SAVE","LATER","v","EMPTY","PRICE","BASE"));
        }
        for(String path:Arrays.asList("","/custom/bin"))assertEquals(Arrays.asList(path,"works"),environment(TemplateBuilder.create().fromImage("image",false).setEnvs(Collections.singletonMap("PATH",path),true).setEnvs(Collections.singletonMap("APP","works"),true),"PATH","APP"));
    }
    @Test void inheritedWorkdirPrecedesCallerStepsAndMutableTagRefreshes()throws Exception{
        try(MockWebServer server=new MockWebServer()){
            server.start();server.enqueue(new MockResponse().setBody("{\"config\":{\"digest\":\"sha256:cfg\"}}"));server.enqueue(new MockResponse().setBody("{\"config\":{\"WorkingDir\":\"/image\",\"Cmd\":[\"old\"]}}"));
            TemplateBuilder b=TemplateBuilder.create().fromImage(server.getHostName()+":"+server.getPort()+"/image").run("pwd").workdir("/override").run("pwd");
            JsonObject first=JsonParser.parseString(b.toJson()).getAsJsonObject();JsonArray steps=first.getAsJsonArray("steps");assertEquals(4,steps.size());assertEquals("/image",steps.get(0).getAsJsonObject().getAsJsonArray("args").get(0).getAsString());assertEquals("RUN",steps.get(1).getAsJsonObject().get("type").getAsString());
            server.enqueue(new MockResponse().setBody("{\"config\":{\"digest\":\"sha256:new\"}}"));server.enqueue(new MockResponse().setBody("{\"config\":{}}"));
            JsonObject second=JsonParser.parseString(b.toJson()).getAsJsonObject();assertFalse(second.has("startCmd"));assertEquals(3,second.getAsJsonArray("steps").size());assertEquals(4,server.getRequestCount());
        }
    }
    @Test void quotedBackslashesAndNonVariableDollarsStayLiteral()throws Exception{
        TemplateBuilder b=TemplateBuilder.create().fromDockerfile("FROM image\nENV REGEX='\\d+' DOUBLE=\"a\\b\"\n").inheritImageConfig(false)
            .setEnvs(Collections.singletonMap("PRICE","cost$5 $$ $? ${MISSING:-literal}"),true);
        assertEquals(Arrays.asList("\\d+","a\\b","cost$5 $$ $? ${MISSING:-literal}"),environment(b,"REGEX","DOUBLE","PRICE"));
    }
}
