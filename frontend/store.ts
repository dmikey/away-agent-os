import {native} from './native';
import {retireUnusedStarterAgents} from './legacy-agents';
import {hydrateMounts,isShared,loadMountedTarget,saveMountedChanges} from './mounted-fs';
import {pathOf,findEntry} from './shared/lib/filesystem';
import {completeSetup} from './setup-state';
import {initialSpace,applyCommand,type Space} from './shared/lib/space';
import {mergeTerminalFiles,normalizeWorkbench} from './shared/lib/workbench';
export async function readSpace():Promise<{state:Space;revision:number;mountRevision?:string}>{
 for(let i=0;i<5;i++){const data=await native().ReadWorkspace();if(data.state){const cleaned=retireUnusedStarterAgents(data.state);if(cleaned!==data.state){try{await native().CommitWorkspace(data.revision,JSON.stringify(cleaned))}catch(e){if(!String(e).includes('Revision conflict'))throw e}continue}const state=await hydrateMounts(data.state);const mountRevision=JSON.stringify([state.mountedFolders,state.workbench.fs.entries.filter((e:any)=>e.mounted).map((e:any)=>[e.path,e.version,e.readOnly])]);return {...data,state,mountRevision}};
 try{return await native().CommitWorkspace(-1,JSON.stringify(normalizeWorkbench(initialSpace())))}catch(e){if(!String(e).includes('Revision conflict'))throw e}}
 throw Error('Workspace is busy. Try again.')
}
export async function command(name:string,args:Record<string,unknown>,actor='you'){
 if(name==='notebook_backend'&&args.provider!=='vm')throw Error('Desktop execution uses the isolated Linux VM. Hosted compute is not connected.');
 if(['file_run','notebook_run','sandbox_shell'].includes(name)&&args.provider==='colab')throw Error('Desktop execution uses the isolated Linux VM.');
 if(['file_run','notebook_run','sandbox_shell'].includes(name)&&!(await native().SandboxStatus()).available)throw Error('The isolated Linux VM runtime is unavailable; execution is disabled.');
 if(name==='setup_complete'&&!(await native().ProviderStatus())[String((args.agent as any)?.provider)])throw Error('Connect your provider before creating the agent.');
 const applied=new Map<string,any>();
 for(let i=0;i<5;i++){const {state,revision}=await readSpace();let target:string|undefined;const raw=name==='file_write'?args.name:args.path||(name==='notebook_update'?state.workbench.book.path:name==='document_write'?state.workbench.document.path:undefined);if(typeof raw==='string')target=pathOf(raw);
 if(target&&isShared(target)&&state.mountedFolders?.length){if(['fs_move','fs_copy','fs_trash','fs_restore'].includes(name))throw Error('Use Terminal or Finder to move or delete shared items.');await loadMountedTarget(state,target,['file_write','fs_write','fs_mkdir','document_write','notebook_update'].includes(name));const own=applied.get(target);if(own&&findEntry(state.workbench.fs!,target)?.version===own.version&&args.expectedVersion!==undefined)args={...args,expectedVersion:own.version};}
 const next=name==='setup_complete'?completeSetup(state,args.agent as any,args.revisit===true):applyCommand(state,name,args,actor);if(['file_run','notebook_run','sandbox_shell'].includes(name)){Object.assign(next.workbench.runs[0],{executionBackend:'apple-vz',sandboxActor:actor})}await saveMountedChanges(state,next,applied);try{return await native().CommitWorkspace(revision,JSON.stringify(next))}catch(e){if(!String(e).includes('Revision conflict'))throw e}}
 throw Error('Workspace changed while saving. Try again.')
}
export function sameOrigin(req:Request){const origin=req.headers.get('origin');return !origin||origin===new URL(req.url).origin}

export async function saveTerminalFiles(baseline:any,files:any[],id:string){
 for(let i=0;i<5;i++){const {state,revision}=await readSpace();const next=structuredClone(state);const conflicts=mergeTerminalFiles(next.workbench,baseline,files,id);try{await native().CommitWorkspace(revision,JSON.stringify(next));return conflicts}catch(e){if(!String(e).includes('Revision conflict'))throw e}}throw Error('Workspace changed while saving terminal files. Try again.');
}
