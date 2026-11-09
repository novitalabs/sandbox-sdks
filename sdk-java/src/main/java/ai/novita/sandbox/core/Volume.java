package ai.novita.sandbox.core;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.CompletableFuture;

/** Control-plane volume operations, aligned with the JS/Python Volume API. */
public final class Volume {
 private final ApiClient client;
 Volume(ApiClient client){this.client=client;}
 private static String path(String id)throws VolumeException{if(id==null||id.trim().isEmpty())throw new VolumeException("volume id is required");return URLEncoder.encode(id,StandardCharsets.UTF_8).replace("+","%20");}

 /** Volume operations are only supported on modern clusters, matching JS/Python. */
 private void requireModern(String feature)throws SandboxException{if(client.isLegacyApiDomain())throw new VolumeException(feature+" is not supported on legacy domains");}

 /**
  * Derives an inode quota from a size quota.
  *
  * <p>The API requires both fields, using the 1000-inodes-per-GiB
  * ratio defined by the volume API contract.
  */
 private static long quotaInodesForSize(long quotaGiB){return quotaGiB*1000L;}

 /**
  * Wraps a failure in {@link VolumeException} unless it already carries a more
  * specific meaning (404, auth, rate limit, timeout), which is preserved so
  * callers can still branch on the original type.
  */
 private static SandboxException wrap(String action, SandboxException e){
  if(e instanceof NotFoundException||e instanceof AuthenticationException||e instanceof RateLimitException||e instanceof TimeoutException)return e;
  return new VolumeException(action+": "+e.getMessage(),e);
 }

 public VolumeInfo create(String name,long quotaGiB)throws SandboxException{
  if(name==null||name.trim().isEmpty())throw new VolumeException("volume name is required");
  if(quotaGiB<0)throw new VolumeException("quotaGiB must be non-negative");
  requireModern("Volume.create");
  java.util.Map<String,Object> p=new java.util.LinkedHashMap<>();
  p.put("name",name);
  // JS omits the quota fields entirely unless a size was provided.
  if(quotaGiB>0){p.put("quotaSizeGiB",quotaGiB);p.put("quotaInodes",quotaInodesForSize(quotaGiB));}
  try{return client.request("POST","/volumes",p,VolumeInfo.class);}catch(SandboxException e){throw wrap("volume creation failed",e);}
 }

 public VolumeInfo create(String name,VolumeOptions options)throws SandboxException{
  if(options==null)return create(name,0);
  if(name==null||name.trim().isEmpty())throw new VolumeException("volume name is required");
  requireModern("Volume.create");
  java.util.Map<String,Object> p=new java.util.LinkedHashMap<>();
  p.put("name",name);
  if(options.hasQuota()){
   if(options.getQuotaGiB()<0||(options.getQuotaInodes()!=null&&options.getQuotaInodes()<0))throw new VolumeException("volume quotas must be non-negative");
   p.put("quotaSizeGiB",options.getQuotaGiB());
   p.put("quotaInodes",options.getQuotaInodes()!=null?options.getQuotaInodes():quotaInodesForSize(options.getQuotaGiB()));
  }
  try{return client.request("POST","/volumes",p,VolumeInfo.class);}catch(SandboxException e){throw wrap("volume creation failed",e);}
 }

 /**
  * Updates the volume quota: PATCH /volumes/{id}. A {@code null} quota sends
  * an empty body like JS/Python ({@code quotaSizeGiB} undefined / {@code None});
  * otherwise the body is {@code {quotaSizeGiB, quotaInodes}} with inodes
  * derived as {@code size*1000}.
  */
 public VolumeInfo updateQuota(String id, Long quotaGiB)throws SandboxException{
  if(quotaGiB!=null&&quotaGiB<0)throw new VolumeException("quotaGiB must be non-negative");
  requireModern("Volume.updateQuota");
  java.util.Map<String,Object> p=new java.util.LinkedHashMap<>();
  if(quotaGiB!=null){
   p.put("quotaSizeGiB",quotaGiB);
   p.put("quotaInodes",quotaInodesForSize(quotaGiB));
  }
  try{return client.request("PATCH","/volumes/"+path(id),p,VolumeInfo.class);}catch(VolumeException e){throw e;}catch(SandboxException e){throw wrap("volume quota update failed",e);}
 }
 public CompletableFuture<VolumeInfo> updateQuotaAsync(String id, Long quotaGiB){return AsyncSupport.supplyAsync(()->{try{return updateQuota(id,quotaGiB);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}

 public VolumeInfo get(String id)throws SandboxException{
  requireModern("Volume.getInfo");
  try{return client.request("GET","/volumes/"+path(id),null,VolumeInfo.class);}catch(VolumeException e){throw e;}catch(SandboxException e){throw wrap("volume lookup failed",e);}
 }
 public VolumeInfo connect(String id)throws SandboxException{return get(id);}
 public CompletableFuture<VolumeInfo> connectAsync(String id){return getAsync(id);}
 public VolumeInfo[] list()throws SandboxException{
  requireModern("Volume.list");
  try{return client.request("GET","/volumes",null,VolumeInfo[].class);}catch(SandboxException e){throw wrap("volume listing failed",e);}
 }

 /** Deletes a volume; 404 reports {@code false} like JS/Python {@code destroy}. */
 public boolean delete(String id)throws SandboxException{
  requireModern("Volume.destroy");
  try{client.request("DELETE","/volumes/"+path(id),null,null);return true;}
  catch(NotFoundException e){return false;}
  catch(VolumeException e){throw e;}
  catch(SandboxException e){throw wrap("volume deletion failed",e);}
 }
 public CompletableFuture<VolumeInfo> getAsync(String id){return AsyncSupport.supplyAsync(()->{try{return get(id);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
 public CompletableFuture<VolumeInfo> createAsync(String name,long quotaGiB){return AsyncSupport.supplyAsync(()->{try{return create(name,quotaGiB);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
 public CompletableFuture<VolumeInfo> createAsync(String name,VolumeOptions options){return AsyncSupport.supplyAsync(()->{try{return create(name,options);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
 public CompletableFuture<Boolean> deleteAsync(String id){return AsyncSupport.supplyAsync(()->{try{return delete(id);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
 public CompletableFuture<VolumeInfo[]> listAsync(){return AsyncSupport.supplyAsync(()->{try{return list();}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
}
