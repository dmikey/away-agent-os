import {native,type NetworkAccessRequest} from './native';
export async function requestAgentNetworkAccess(actor:string,url:string,reason:string,signal?:AbortSignal):Promise<NetworkAccessRequest>{
 if(signal?.aborted)throw new DOMException('Cancelled','AbortError');
 const request=await native().RequestNetworkAccess(actor,url,reason);
 if(signal?.aborted){await native().CancelNetworkAccess(actor,request.id);throw new DOMException('Cancelled','AbortError')}
 if(request.status!=='pending')return request;
 let abort:()=>void=()=>{};
 const cancelled=new Promise<never>((_,reject)=>{abort=()=>{native().CancelNetworkAccess(actor,request.id).catch(()=>{});reject(new DOMException('Cancelled','AbortError'))};signal?.addEventListener('abort',abort,{once:true})});
 try{return await Promise.race([native().WaitNetworkAccess(request.id),cancelled])}finally{signal?.removeEventListener('abort',abort)}
}
export async function fetchAgentWeb(actor:string,url:string,maxChars:number,signal?:AbortSignal){
 const access=await requestAgentNetworkAccess(actor,url,'Read this page as part of the current task.',signal);
 if(access.status!=='approved')return {error:'Network access '+access.status+'. Continue without this domain; respect the decision.',networkAccess:access};
 return native().FetchWeb(actor,url,maxChars);
}
