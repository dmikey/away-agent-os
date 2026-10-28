import {native} from './native';
import {readFs,pathOf,ensureDirs,findEntry,type FileSystem} from './shared/lib/filesystem';
import {normalizeWorkbench,starterWorkbench} from './shared/lib/workbench';
export const SHARED='/workspace/Shared';
export const isShared=(p:unknown)=>typeof p==='string'&&(p===SHARED||p.startsWith(SHARED+'/'));
export async function hydrateMounts(state:any){
 normalizeWorkbench(state);const fs=state.workbench.fs as FileSystem;
 const folders=await native().SharedFolders();state.mountedFolders=folders.map(({name,path,readOnly,available})=>({name,path,readOnly,available}));
 const cached=fs.entries.filter(e=>e.mounted&&e.kind==='file');fs.entries=fs.entries.filter(e=>!e.mounted);
 if(folders.length){ensureDirs(fs,SHARED);findEntry(fs,SHARED)!.mounted=true;for(const f of folders){ensureDirs(fs,f.path);Object.assign(findEntry(fs,f.path)!,{mounted:true,readOnly:f.readOnly})}}
 for(const item of cached){if(!folders.some(f=>f.available&&item.path.startsWith(f.path+'/')))continue;try{const {entry}=await native().ReadShared(item.path,true);ensureDirs(fs,item.path.slice(0,item.path.lastIndexOf('/')));fs.entries.push(entry);for(const d of fs.entries.filter(e=>isShared(e.path)))d.mounted=true}catch{}}
 if(isShared(state.workbench.activeFile)&&!findEntry(fs,state.workbench.activeFile))state.workbench.activeFile=undefined;
 const defaults=starterWorkbench();for(const kind of ['book','document'] as const)if(isShared(state.workbench[kind].path)&&!findEntry(fs,state.workbench[kind].path)){state.workbench[kind]=defaults[kind];state.workbench[kind].path=fs.entries.find(e=>!isShared(e.path)&&e.path.endsWith(kind==='book'?'.ipynb':'.html'))?.path}
 for(const d of fs.entries.filter(e=>e.mounted&&e.kind==='directory'))d.id='mount-dir:'+d.path;
 return normalizeWorkbench(state);
}
export async function readWorkspacePath(fs:FileSystem,args:Record<string,any>){const p=pathOf(args.path||'/workspace');if(isShared(p)&&!(await native().SharedFolders()).length)return readFs(fs,args);if(p===SHARED){const folders=await native().SharedFolders();return {path:p,entries:folders.map(f=>({path:f.path,name:f.name,kind:'directory',readOnly:f.readOnly,available:f.available}))}}if(isShared(p))return native().ReadShared(p,true);return readFs(fs,args)}
export async function loadMountedTarget(state:any,p:string,allowMissing:boolean){if(p===SHARED)return;try{const {entry}=await native().ReadShared(p,true);const fs=state.workbench.fs as FileSystem;ensureDirs(fs,p.slice(0,p.lastIndexOf('/')));fs.entries=fs.entries.filter(e=>e.path!==p);fs.entries.push(entry);for(const d of fs.entries.filter(e=>isShared(e.path)))d.mounted=true}catch(e){if(!allowMissing)throw e}}
// The mount registry remains outside agent state. Ordinary editor saves are routed to its confined bridge.
export async function saveMountedChanges(before:any,next:any,applied:Map<string,any>){
 if(!before.mountedFolders?.length)return;
 const old=before.workbench.fs as FileSystem,fs=next.workbench.fs as FileSystem;
 for(const e of old.entries.filter(e=>isShared(e.path)))if(!fs.entries.some(n=>n.path===e.path&&!n.trashed))throw Error('Move or delete mounted items in Terminal or Finder. Unshare folders in Shared folders.');
 for(const e of [...fs.entries].filter(e=>isShared(e.path)&&!e.trashed).sort((a,b)=>a.path.length-b.path.length)){
  if(e.path===SHARED)continue;const base=findEntry(old,e.path);
  if(e.kind==='directory'){if(!base)await native().MkdirShared(e.path);continue}
  if(base&&base.content===e.content&&base.encoding===e.encoding){e.version=base.version;e.modified=base.modified;continue}
  const saved=await native().WriteShared(e.path,e.content,e.encoding,base?.version||0);Object.assign(e,saved);applied.set(e.path,saved);
 }
 for(const e of fs.entries.filter(e=>isShared(e.path)))e.mounted=true;
 normalizeWorkbench(next);
}
