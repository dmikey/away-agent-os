import {native} from './native';
import {requestAgentNetworkAccess} from './network-access';
import {command} from './store';
export async function visitAgentUrl(actor:string,url:string,reason:string,signal:AbortSignal,maxChars=12000){
 const target=new URL(url.includes('://')?url:'https://'+url);target.hash='';url=target.href;
 await command('browser_navigate',{url},actor);
 try{
  const access=await requestAgentNetworkAccess(actor,url,reason,signal);
  if(access.status!=='approved'){
   const result={url,actor,error:'Network access '+access.status+'. The user did not grant access to this domain.',access:access.status};
   await command('browser_result',{page:result},actor);return result;
  }
  if(signal.aborted)throw new DOMException('Cancelled','AbortError');
  const page={...await native().FetchWeb(actor,url,maxChars),url,actor,access:'approved'};
  if(signal.aborted)throw new DOMException('Cancelled','AbortError');
  await command('browser_result',{page},actor);return page;
 }catch(e){if(signal.aborted)throw e;const page={url,actor,error:e instanceof Error?e.message:'Web request failed'};await command('browser_result',{page},actor);return page}
}
