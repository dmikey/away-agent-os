import {useEffect,useState} from 'react';
import {ExternalLink,KeyRound,Loader2,Check,ShieldCheck} from 'lucide-react';
import {native} from './native';
import {providerInfo} from './providers';
export function ProviderConnection({provider,onConnected,onBusy}:{provider:string;onConnected:(models:string[])=>void;onBusy?:(busy:boolean)=>void}){
 const [key,setKey]=useState(''),[saved,setSaved]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState(''),[success,setSuccess]=useState(false);
 const info=providerInfo(provider);
 useEffect(()=>{let live=true;native().ProviderStatus().then(s=>{if(live)setSaved(!!s[provider])}).catch(()=>{});return()=>{live=false}},[provider]);
 const connect=async(e:React.FormEvent)=>{e.preventDefault();if(busy)return;setBusy(true);onBusy?.(true);setError('');try{const data=await native().ConnectProvider(provider,key.trim());setKey('');setSaved(true);setSuccess(true);onConnected(data.models)}catch(e){const message=e instanceof Error?e.message:typeof e==='string'?e:'';setError(message||'Could not connect. Check your API key, API access, and connection, then try again.')}finally{setBusy(false);onBusy?.(false)}};
 return <form className="provider-connect" onSubmit={connect}>
 <div className="setup-key-link"><KeyRound size={18}/><div><strong>A key from your {provider} account</strong><p>Create a key, then paste it here to connect.</p></div><button type="button" aria-label={'Get '+provider+' API key'} disabled={busy} onClick={()=>native().OpenExternal(info.keysUrl).catch(()=>setError('Could not open your browser. Try again.'))}>Get a key <ExternalLink size={12}/></button></div>
 <label>API key<input type="password" autoComplete="off" autoCorrect="off" spellCheck={false} value={key} onChange={e=>{setKey(e.target.value);setSuccess(false)}} placeholder={saved?'Saved in macOS Keychain · paste to replace':'Paste your API key'} disabled={busy}/></label>
 <p className="setup-key-note"><ShieldCheck size={14}/> Stored in macOS Keychain. Never in your files or agent instructions.</p>
 <p className="setup-billing-note">API usage is billed by {provider}. Connecting checks your key and available models without generating a reply.</p>
 {error&&<p className="setup-error" role="alert">{error}</p>}
 <button className="setup-primary" disabled={busy||(!key.trim()&&!saved)}>{busy?<><Loader2 size={15} className="spin"/> Checking connection…</>:success?<><Check size={15}/> Connected · check again</>:saved&&!key.trim()?'Use saved key':'Connect '+provider}</button>
 </form>;
}
