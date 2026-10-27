import {useEffect,useState} from 'react';
import {native} from './native';
export function SafeWebPreview({url}:{url:string}){
 const [text,setText]=useState(''),[error,setError]=useState(''),[busy,setBusy]=useState(true),[truncated,setTruncated]=useState(false);
 useEffect(()=>{let active=true;setBusy(true);setError('');native().FetchWeb('you',url,30000).then(r=>{if(active){setText(r.text);setTruncated(r.truncated)}}).catch(e=>{if(active)setError(String(e))}).finally(()=>{if(active)setBusy(false)});return()=>{active=false}},[url]);
 return <div className="native-web-preview"><header><strong>Readable web</strong><small>Go-controlled fetch · untrusted source text</small></header>{busy?<p>Fetching through the network gateway…</p>:error?<div role="alert"><p>{error}</p><small>Approve this exact domain in Local runtime → Network → You. External scripts and pages are never embedded in the privileged desktop WebView.</small></div>:<><pre>{text}</pre>{truncated&&<small>Trimmed at the text limit.</small>}</>}</div>;
}
