import {native,type BrowserAction,type BrowserFrame} from './native';
// One observable browser VM. Serialize UI and agent actions so they share a page.
let tail:Promise<unknown>=Promise.resolve(),owner='',target='';
function enqueue(actor:string,requested:BrowserAction,reuse=false):Promise<BrowserFrame>{
 const result=tail.catch(()=>{}).then(async()=>{
  if(owner&&owner!==actor){await native().CloseBrowser(owner);target=''}
  if(!reuse&&requested.kind!=='navigate'&&(!target||owner!==actor))throw Error('Open a page with browser_navigate before interacting.');
  const action:BrowserAction=reuse&&owner===actor&&target===requested.url?{kind:'frame'}:requested;
  owner=actor;
  try{const frame=await native().BrowserAction(actor,action);if(action.kind==='navigate')target=action.url||'';return frame}catch(e){target='';throw e}
 });tail=result;return result;
}
export function sandboxBrowser(actor:string,action:BrowserAction){return enqueue(actor,action)}
export function openSandboxBrowser(actor:string,url:string){return enqueue(actor,{kind:'navigate',url},true)}
export function closeSandboxBrowser(actor:string){const result=tail.catch(()=>{}).then(async()=>{if(owner===actor){await native().CloseBrowser(actor);owner='';target=''}});tail=result;return result}
export function readableBrowserResult(frame:BrowserFrame,maxChars=30000){return {url:frame.url,title:frame.title,text:frame.text.slice(0,maxChars),links:frame.links,controls:frame.controls,width:frame.width,height:frame.height,sandboxed:frame.sandboxed,source:'isolated-chromium-dom',truncated:frame.text.length>maxChars}}
