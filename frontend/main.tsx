import React,{useEffect,useState,lazy,Suspense} from 'react';
import {createRoot} from 'react-dom/client';
import Desktop from './shared/app/page';
import {Onboarding} from './onboarding';
import {needsSetup,shouldOfferSetup} from './setup-state';
import './shared/app/globals.css';
import './native.css';
import {installTransport} from './transport';
import {native,type SandboxInfo} from './native';
import {readSpace} from './store';
import {NetworkSettings} from './network';
import {CloudSettings} from './cloud-settings';
import {NetworkApproval} from './network-approval';
const Terminal=lazy(()=>import('./terminal').then(module=>({default:module.Terminal})));
installTransport();
function NativeDesktop(){
 const [setup,setSetup]=useState<boolean|null>(null),[loadError,setLoadError]=useState(''),[firstAgent,setFirstAgent]=useState(''),[revisit,setRevisit]=useState(false);
 const load=()=>{setLoadError('');Promise.all([readSpace(),native().ProviderStatus()]).then(([{state},connections])=>{setRevisit(!needsSetup(state));setSetup(shouldOfferSetup(state,connections))}).catch(()=>setLoadError('Your workspace could not be opened. Try again.'))};
 useEffect(load,[]);
 useEffect(()=>{if(setup===true)native().FrontendReady().catch(()=>{})},[setup]);
 const [sandbox,setSandbox]=useState<SandboxInfo|null>(null),[details,setDetails]=useState(false),[panel,setPanel]=useState<'isolation'|'network'|'terminal'|'cloud'>('isolation');
 useEffect(()=>{if(!details)return;const key=(e:KeyboardEvent)=>{if(e.key==='Escape')setDetails(false)};document.addEventListener('keydown',key);return()=>document.removeEventListener('keydown',key)},[details]);
 useEffect(()=>{native().SandboxStatus().then(status=>{setSandbox(status)}).catch(()=>{});},[]);
 if(setup===null)return <main className="setup-loading" role="status"><span>✿</span><p>{loadError||'Opening your space…'}</p>{loadError&&<button onClick={load}>Try again</button>}</main>;
 if(setup)return <Onboarding onCancel={revisit?()=>setSetup(false):undefined} onComplete={id=>{setFirstAgent(id);setSetup(false)}}/>;
 return <><NetworkApproval/><Desktop initialAgentId={firstAgent} onSetup={()=>{setRevisit(true);setSetup(true)}}/><button className="native-sandbox-status" onClick={()=>setDetails(true)}>✳ Local workspace · {sandbox?.available?'VM isolation':'Execution locked'}</button>{details&&<div className="native-sandbox-backdrop" onClick={()=>setDetails(false)}><section role="dialog" aria-modal="true" aria-label="Workspace runtime" className="native-sandbox-dialog" onClick={e=>e.stopPropagation()}><header><strong>Workspace runtime</strong><button aria-label="Close agent isolation" onClick={()=>setDetails(false)}>×</button></header><div className="native-runtime-tabs" role="tablist" aria-label="Local runtime settings"><button role="tab" aria-selected={panel==='isolation'} onClick={()=>setPanel('isolation')}>Isolation</button><button role="tab" aria-selected={panel==='network'} onClick={()=>setPanel('network')}>Network</button><button role="tab" aria-selected={panel==='cloud'} onClick={()=>setPanel('cloud')}>Cloud</button><button role="tab" aria-selected={panel==='terminal'} onClick={()=>setPanel('terminal')}>Terminal</button></div>{panel==='cloud'?<CloudSettings/>:panel==='network'?<NetworkSettings/>:panel==='terminal'?<Suspense fallback={<p>Opening terminal…</p>}><Terminal/></Suspense>:<><p>{sandbox?.reason||'Checking sandbox availability…'}</p><dl><dt>Runtime</dt><dd>Dedicated Linux VM</dd><dt>Host shell / processes</dt><dd>Denied</dd><dt>Host folder access</dt><dd>Explicit grants only</dd><dt>Execution fallback</dt><dd>Disabled</dd></dl><small>Agents can collaborate and edit the virtual workspace. Each actor has an isolated Python kernel. VMs see the complete /workspace boundary, including only the Mac folders you explicitly share.</small><div className="native-runtime-reset"><button className="native-button" onClick={()=>native().ResetSandboxes().then(()=>setSandbox(current=>current?{...current,reason:'VMs stopped. Python variables reset. The next run boots a fresh isolated guest.'}:null))}>Reset all VMs</button></div></>}</section></div>}</>;
}
createRoot(document.getElementById('root')!).render(<NativeDesktop/>);
