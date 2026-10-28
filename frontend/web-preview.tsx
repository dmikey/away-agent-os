import {useEffect,useState} from 'react';
import {fetchAgentWeb} from './network-access';
import type {BrowserPage} from './shared/lib/space';
export function SafeWebPreview({url,actor='you',page,owner='You'}:{url:string;actor?:string;page?:BrowserPage;owner?:string}){
 const [text,setText]=useState(''),[error,setError]=useState(''),[busy,setBusy]=useState(true),[truncated,setTruncated]=useState(false),[status,setStatus]=useState<number>();
 useEffect(()=>{let active=true;const controller=new AbortController();setBusy(true);setError('');
 if(page?.url===url&&page.actor===actor){setText(page.text||'');setError(page.error||'');setTruncated(!!page.truncated);setStatus(page.status);setBusy(false);return}
 if(actor!=='you'){setText('');return}
 fetchAgentWeb(actor,url,30000,controller.signal).then(r=>{if(!active)return;if('error' in r)setError(r.error);else{setText(r.text);setTruncated(r.truncated);setStatus(r.status)}}).catch(e=>{if(active)setError(String(e))}).finally(()=>{if(active)setBusy(false)});return()=>{active=false;controller.abort()}},[url,actor,page]);
 return <div className="native-web-preview"><header><strong>{page?.source==='isolated-chromium-dom'?'Readable page':'Readable web'}{status?' · HTTP '+status:''}</strong><small>{owner} · {page?.source==='isolated-chromium-dom'?'Sandboxed DOM':'Go-controlled fetch'} · untrusted source text</small></header>{busy?<p>Waiting for approval or reading through the network gateway…</p>:error?<div role="alert"><p>{error}</p><small>Access is scoped to the requesting agent. Use the permission dialog to allow a domain; manage grants in Local runtime → Network.</small></div>:<><>{page?.renderError&&<small>Rendering unavailable: {page.renderError}. Showing the Go text fetch.</small>}</><pre>{text}</pre>{truncated&&<small>Trimmed at the text limit.</small>}</>}</div>;
}
