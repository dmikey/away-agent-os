import {native} from './native';
import {completeSetup} from './setup-state';
import {initialSpace,applyCommand,type Space} from './shared/lib/space';
import {mergeTerminalFiles,normalizeWorkbench} from './shared/lib/workbench';
export async function readSpace():Promise<{state:Space;revision:number}>{
 for(let i=0;i<5;i++){const data=await native().ReadWorkspace();if(data.state)return {...data,state:normalizeWorkbench(data.state)};
 try{return await native().CommitWorkspace(-1,JSON.stringify(normalizeWorkbench(initialSpace())))}catch(e){if(!String(e).includes('Revision conflict'))throw e}}
 throw Error('Workspace is busy. Try again.')
}
export async function command(name:string,args:Record<string,unknown>,actor='you'){
 if(name==='notebook_backend'&&args.provider!=='vm')throw Error('Desktop execution uses the isolated Linux VM. Hosted compute is not connected.');
 if(['file_run','notebook_run','sandbox_shell'].includes(name)&&args.provider==='colab')throw Error('Desktop execution uses the isolated Linux VM.');
 if(['file_run','notebook_run','sandbox_shell'].includes(name)&&!(await native().SandboxStatus()).available)throw Error('The isolated Linux VM runtime is unavailable; execution is disabled.');
 if(name==='setup_complete'&&!(await native().ProviderStatus())[String((args.agent as any)?.provider)])throw Error('Connect your provider before creating the agent.');
 for(let i=0;i<5;i++){const {state,revision}=await readSpace();const next=name==='setup_complete'?completeSetup(state,args.agent as any,args.revisit===true):applyCommand(state,name,args,actor);if(['file_run','notebook_run','sandbox_shell'].includes(name)){Object.assign(next.workbench.runs[0],{executionBackend:'apple-vz',sandboxActor:actor})}try{return await native().CommitWorkspace(revision,JSON.stringify(next))}catch(e){if(!String(e).includes('Revision conflict'))throw e}}
 throw Error('Workspace changed while saving. Try again.')
}
export function sameOrigin(req:Request){const origin=req.headers.get('origin');return !origin||origin===new URL(req.url).origin}

export async function saveTerminalFiles(baseline:any,files:any[],id:string){
 for(let i=0;i<5;i++){const {state,revision}=await readSpace();const next=structuredClone(state);const conflicts=mergeTerminalFiles(next.workbench,baseline,files,id);try{await native().CommitWorkspace(revision,JSON.stringify(next));return conflicts}catch(e){if(!String(e).includes('Revision conflict'))throw e}}throw Error('Workspace changed while saving terminal files. Try again.');
}
