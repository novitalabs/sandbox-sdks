package ai.novita.sandbox.core;

import com.google.gson.*;

final class JobErrors {
    private JobErrors() { }
    static SandboxException from(int code,String body){
        String reason="",detail="";
        try{JsonElement parsed=JsonParser.parseString(body);if(parsed.isJsonObject()){JsonObject o=parsed.getAsJsonObject();reason=string(o,"reason");detail=string(o,"message");}}catch(RuntimeException ignored){ }
        String message=code+": "+(reason.isEmpty()?(detail.isEmpty()?"request failed":detail):reason+(detail.isEmpty()?"":" - "+detail));
        switch(reason){
            case "RESOURCE_NOT_FOUND":return new NotFoundException(message);
            case "MIGRATION_TEMPLATE_ALIAS_CONFLICT":return new TemplateAliasConflictException(message);
            case "MIGRATION_RESOURCE_ALREADY_MIGRATING":return new ResourceAlreadyMigratingException(message);
            case "MIGRATION_PAST_CANCEL_LINE":return new CancelTooLateException(message);
            case "MIGRATION_SHARED_DEPENDENCY":return new SharedDependencyException(message);
            case "MIGRATION_CONFLICT":return new MigrationConflictException(message,reason);
            case "MIGRATION_NOT_SUPPORTED":return new MigrationNotSupportedException(message);
            case "UNAUTHORIZED":return new AuthenticationException(message);
            default:break;
        }
        switch(code){
            case 429:return new RateLimitException(message);
            case 401:return new AuthenticationException(message);
            case 501:return new MigrationNotSupportedException(message);
            case 409:return new MigrationConflictException(message,reason.isEmpty()?"MIGRATION_CONFLICT":reason);
            case 400:return new InvalidArgumentException(message);
            default:return new SandboxException(message);
        }
    }
    private static String string(JsonObject o,String key){JsonElement e=o.get(key);return e!=null&&e.isJsonPrimitive()&&e.getAsJsonPrimitive().isString()?e.getAsString():"";}
}
