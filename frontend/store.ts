import {native} from './native';
import {initialSpace,applyCommand,type Space} from './shared/lib/space';
import {normalizeWorkbench} from './shared/lib/workbench';
export async function readSpace():Promise<{state:Space;revision:number}>{
 for(let i=0;i<5;i++){const data=await native().ReadWorkspace();if(data.state)return {...data,state:normalizeWorkbench(data.state)};
 try{return await native().CommitWorkspace(-1,JSON.stringify(normalizeWorkbench(initialSpace())))}catch(e){if(!String(e).includes('Revision conflict'))throw e}}
 throw Error('Workspace is busy. Try again.')
}
export async function command(name:string,args:Record<string,unknown>,actor='you'){
 if(name==='notebook_backend'&&args.provider!=='vm')throw Error('Desktop execution uses the isolated Linux VM. Hosted compute is not connected.');
 if(['file_run','notebook_run','sandbox_shell'].includes(name)&&args.provider==='colab')throw Error('Desktop execution uses the isolated Linux VM.');
 if(['file_run','notebook_run','sandbox_shell'].includes(name)&&!(await native().SandboxStatus()).available)throw Error('The isolated Linux VM runtime is unavailable; execution is disabled.');
 for(let i=0;i<5;i++){const {state,revision}=await readSpace();const next=applyCommand(state,name,args,actor);if(['file_run','notebook_run','sandbox_shell'].includes(name)){Object.assign(next.workbench.runs[0],{executionBackend:'apple-vz',sandboxActor:actor})}try{return await native().CommitWorkspace(revision,JSON.stringify(next))}catch(e){if(!String(e).includes('Revision conflict'))throw e}}
 throw Error('Workspace changed while saving. Try again.')
}
export function sameOrigin(req:Request){const origin=req.headers.get('origin');return !origin||origin===new URL(req.url).origin}
