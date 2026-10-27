import React,{useEffect,useState} from 'react';
import {createRoot} from 'react-dom/client';
import Desktop from './shared/app/page';
import './shared/app/globals.css';
import './native.css';
import {installTransport} from './transport';
import {native,type SandboxInfo} from './native';
import {readSpace} from './store';
import {NetworkSettings} from './network';
import {Terminal} from './terminal';
installTransport();
function NativeDesktop(){
 const [sandbox,setSandbox]=useState<SandboxInfo|null>(null),[details,setDetails]=useState(false),[panel,setPanel]=useState<'isolation'|'network'|'terminal'>('isolation');
 useEffect(()=>{if(!details)return;const key=(e:KeyboardEvent)=>{if(e.key==='Escape')setDetails(false)};document.addEventListener('keydown',key);return()=>document.removeEventListener('keydown',key)},[details]);
 useEffect(()=>{Promise.all([readSpace(),native().SandboxStatus()]).then(([,status])=>{setSandbox(status)}).catch(()=>{});},[]);
 return <><Desktop/><button className="native-sandbox-status" onClick={()=>setDetails(true)}>✳ Local workspace · {sandbox?.available?'VM isolation':'Execution locked'}</button>{details&&<div className="native-sandbox-backdrop" onClick={()=>setDetails(false)}><section role="dialog" aria-modal="true" aria-label="Local agent runtime" className="native-sandbox-dialog" onClick={e=>e.stopPropagation()}><header><strong>Local agent runtime</strong><button aria-label="Close agent isolation" onClick={()=>setDetails(false)}>×</button></header><div className="native-runtime-tabs" role="tablist" aria-label="Local runtime settings"><button role="tab" aria-selected={panel==='isolation'} onClick={()=>setPanel('isolation')}>Isolation</button><button role="tab" aria-selected={panel==='network'} onClick={()=>setPanel('network')}>Network</button><button role="tab" aria-selected={panel==='terminal'} onClick={()=>setPanel('terminal')}>Terminal</button></div>{panel==='network'?<NetworkSettings/>:panel==='terminal'?<Terminal/>:<><p>{sandbox?.reason||'Checking sandbox availability…'}</p><dl><dt>Runtime</dt><dd>Dedicated Linux VM</dd><dt>Host shell / processes</dt><dd>Denied</dd><dt>Host directory mounts</dt><dd>None</dd><dt>Execution fallback</dt><dd>Disabled</dd></dl><small>Agents can collaborate and edit the virtual workspace. Each actor has an isolated Python kernel. Only workspace files and results cross the VM boundary.</small><div className="native-runtime-reset"><button className="native-button" onClick={()=>native().ResetSandboxes().then(()=>setSandbox(current=>current?{...current,reason:'VMs stopped. Python variables reset. The next run boots a fresh isolated guest.'}:null))}>Reset all VMs</button></div></>}</section></div>}</>;
}
createRoot(document.getElementById('root')!).render(<NativeDesktop/>);
