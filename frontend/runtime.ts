import {useEffect,useRef,useState} from 'react';
import type {Workbench} from './shared/lib/workbench';
import {native} from './native';
export function usePythonRuntime(workbench:Workbench,act:(name:string,args:Record<string,unknown>)=>Promise<any>,_compute:unknown){
 const owner=useRef(crypto.randomUUID()),active=useRef<string|null>(null),alive=useRef(true);const [status,setStatus]=useState('Python · isolated Linux VM');
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;if(active.current)native().CancelRequest('vm:'+active.current).catch(()=>{})}},[]);
 useEffect(()=>{if(workbench.kernel?.owner!==owner.current)return;const timer=setInterval(()=>act('notebook_heartbeat',{owner:owner.current}).catch(()=>{}),15000);return()=>clearInterval(timer)},[workbench.kernel?.owner,act]);
 useEffect(()=>{const queued=workbench.runs.find(r=>r.status==='queued');if(!queued||active.current)return;active.current=queued.id;setStatus('Starting isolated Linux VM…');
 (async()=>{try{await act('notebook_claim',{runId:queued.id,owner:owner.current});if(active.current!==queued.id)return;setStatus('Python · running inside Linux VM');const result=await native().ExecuteRun(queued.id,owner.current);if(active.current!==queued.id||!alive.current)return;await act('notebook_result',{runId:queued.id,owner:owner.current,...result});setStatus(result.error?'VM · execution failed':'Python · VM idle')}catch(e){if(active.current===queued.id&&alive.current){await act('notebook_result',{runId:queued.id,owner:owner.current,error:true,output:String(e)}).catch(()=>{});setStatus('VM unavailable · no host fallback')}}finally{if(active.current===queued.id)active.current=null}})();
 },[workbench.runs,act]);
 const stop=()=>{const id=active.current;active.current=null;if(id)native().CancelRequest('vm:'+id).catch(()=>{});act('notebook_cancel',{}).catch(()=>{});setStatus('Stopped · isolated VM reset')};
 const restart=()=>{stop();native().ResetSandboxes().then(()=>setStatus('Isolated VMs reset')).catch(()=>setStatus('Could not reset VMs'))};
 return {status,stop,restart};
}
