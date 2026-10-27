import {native} from './native';
import {GET,POST} from './shared/app/api/space/route';
import {POST as run} from './shared/app/api/run/route';
// Keep the shared UI and validated command path. The native backend persists every revision.
export function installTransport(){
 const original=window.fetch.bind(window);
 window.open=((url?:string|URL)=>{if(url)native().OpenExternal(new URL(String(url),location.href).href).catch(()=>{});return null}) as typeof window.open;
 window.fetch=(async(input:RequestInfo|URL,init?:RequestInit)=>{
 const raw=input instanceof Request?input.url:String(input);const url=new URL(raw,location.href);
 if(url.origin===location.origin&&url.pathname.startsWith('/api/')){
 const req=new Request(new URL(url.pathname+url.search,'http://away.desktop'),input instanceof Request?input:init);
 try{
 if(url.pathname==='/api/space')return req.method==='GET'?GET():POST(req);
 if(url.pathname==='/api/run')return run(req);
 if(url.pathname==='/api/colab')return Response.json(req.method==='GET'?{clientId:''}:{error:'Colab OAuth is configured for the hosted app. Desktop OAuth support is not connected yet.'},{status:req.method==='GET'?200:409});
 return Response.json({error:'Unknown desktop endpoint'},{status:404});
 }catch(e){return Response.json({error:String(e)},{status:500})}
 }
 if(url.href==='https://api.openai.com/v1/chat/completions'){
 const req=new Request(url,input instanceof Request?input:init);const id=crypto.randomUUID();
 if(req.signal.aborted)throw new DOMException('Cancelled','AbortError');
 const abort=()=>{native().CancelRequest(id).catch(()=>{})};req.signal.addEventListener('abort',abort,{once:true});
 try{const result=await native().Complete(id,req.headers.get('Authorization')?.replace(/^Bearer /,'')||'',await req.text());if(req.signal.aborted)throw new DOMException('Cancelled','AbortError');return new Response(result.body,{status:result.status,headers:{'Content-Type':'application/json'}})}finally{req.signal.removeEventListener('abort',abort)}
 }
 return original(input,init);
 }) as typeof fetch;
}
