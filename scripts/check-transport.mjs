import fs from 'node:fs';import path from 'node:path';import vm from 'node:vm';import assert from 'node:assert/strict';
import ts from '../frontend/node_modules/typescript/lib/typescript.js';
const cache=new Map();let snapshot={state:null,revision:-1};
const connections={OpenAI:false,Anthropic:false,xAI:false};
const bindings={async SharedFolders(){return []},async ProviderStatus(){return {...connections}},async CompleteProvider(id,provider,key,body){assert.equal(key,'','saved connections must not be replaced with an empty Bearer header');return {status:200,body:JSON.stringify({choices:[{message:{content:provider+' reply'}}]})}},async SandboxStatus(){return {available:true}},async ReadWorkspace(){return structuredClone(snapshot)},async CommitWorkspace(revision,json){await new Promise(r=>setTimeout(r,1));if(revision!==snapshot.revision)throw Error('Revision conflict; retry');snapshot={state:JSON.parse(json),revision:revision+1};return structuredClone(snapshot)}};
const window={go:{main:{App:bindings}},fetch:async()=>{throw Error('Unexpected network call')}};
const globals={window,fetch:(...args)=>window.fetch(...args),location:{href:'http://wails.localhost/',origin:'http://wails.localhost'},crypto,structuredClone,TextEncoder,TextDecoder,Uint8Array,Date,atob,btoa,Response,Request,URL,AbortSignal,DOMException,setTimeout};
function load(file){file=path.resolve(file);if(cache.has(file))return cache.get(file).exports;const module={exports:{}};cache.set(file,module);const output=ts.transpileModule(fs.readFileSync(file,'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText;vm.runInNewContext('(function(require,module,exports){'+output+'\n})',globals)(p=>p==='cloudflare:workers'?{env:{}}:load(path.resolve(path.dirname(file),p+'.ts')),module,module.exports);return module.exports}
load('frontend/transport.ts').installTransport();
let response=await window.fetch('/api/space');assert(response.ok);let data=await response.json();assert.equal(data.revision,0);assert(data.state.workbench.fs.entries.length>0);
const act=(name,args)=>window.fetch('/api/space',{method:'POST',body:JSON.stringify({name,args}),headers:{'Content-Type':'application/json'}});
response=await act('fs_write',{path:'/workspace/native-check.txt',content:'Saved through Go contract',expectedVersion:0});assert(response.ok);data=await response.json();assert.equal(data.revision,1);
response=await window.fetch('/api/space');data=await response.json();assert(data.state.workbench.fs.entries.some(e=>e.path==='/workspace/native-check.txt'&&e.content==='Saved through Go contract'));
const parallel=await Promise.all([act('window_control',{window:'code',action:'show'}),act('window_control',{window:'writer',action:'show'})]);assert(parallel.every(r=>r.ok));assert.equal(snapshot.revision,3);
response=await act('sandbox_shell',{command:'pwd'});assert(response.ok);data=await response.json();assert.equal(data.state.workbench.runs[0].executionBackend,'apple-vz');assert.equal(data.state.workbench.runs[0].sandboxActor,'you');await act('notebook_cancel',{});
response=await act('unknown',{});assert.equal(response.status,400);
await act('agent_save',{agent:{name:'Milo',provider:'OpenAI',model:'gpt-4.1-mini'}});
response=await window.fetch('/api/run',{method:'POST',body:JSON.stringify({agentId:snapshot.state.agents[0].id})});assert.equal(response.status,409);
response=await window.fetch('/api/colab',{method:'POST',body:'{}'});assert.equal(response.status,409);
console.log('Desktop transport: initialization, persistence contract, concurrent saves, validation and unavailable-provider errors passed.');

const {completeSetup,needsSetup}=load('frontend/setup-state.ts');
const {initialSpace}=load('frontend/shared/lib/space.ts');
const fresh=initialSpace();assert(needsSetup(fresh));assert.equal(fresh.agents.length,0);assert.deepEqual(Object.keys(fresh.windows).filter(id=>fresh.windows[id].open),['chat']);
const legacy=structuredClone(fresh);delete legacy.setup;assert(!needsSetup(legacy),'existing workspaces must not be forced through setup');
for(const provider of ['OpenAI','Anthropic','xAI']){
 const ready=completeSetup(fresh,{name:'Fern',provider,model:'test-model',instructions:'Help me build.',skills:'Read first.'});
 assert(!needsSetup(ready));assert.equal(ready.agents.length,1);assert.equal(ready.agents[0].provider,provider);assert.equal(ready.setup.firstAgentId,ready.agents[0].id);
 assert.deepEqual(Object.keys(ready.windows).filter(id=>ready.windows[id].open),['chat']);
 assert.throws(()=>completeSetup(ready,ready.agents[0]),/already complete/);
 const res=await window.fetch({'OpenAI':'https://api.openai.com','Anthropic':'https://api.anthropic.com','xAI':'https://api.x.ai'}[provider]+'/v1/chat/completions',{method:'POST',body:'{}'});assert(res.ok);assert((await res.json()).choices[0].message.content.includes(provider));
}
assert.throws(()=>completeSetup(fresh,{name:'',provider:'OpenAI',model:'x',instructions:'x'}),/name/);
console.log('First-run state: empty start, atomic completion, provider persistence, duplicate rejection, Messenger-only windows and legacy preservation passed.');
const {command}=load('frontend/store.ts');snapshot={state:initialSpace(),revision:0};
const first={name:'First',provider:'Anthropic',model:'claude-test',instructions:'Collaborate carefully.'};
await assert.rejects(()=>command('setup_complete',{agent:first}),/Connect your provider/);assert.equal(snapshot.revision,0);
connections.Anthropic=true;
const finishes=await Promise.allSettled([command('setup_complete',{agent:first}),command('setup_complete',{agent:first})]);
assert.equal(finishes.filter(r=>r.status==='fulfilled').length,1);assert.equal(snapshot.state.agents.length,1);assert(!JSON.stringify(snapshot.state).includes('fixture-key'));
console.log('Setup transaction: connection required, simultaneous completion creates exactly one agent, no credential in saved state.');

response=await window.fetch('/api/run',{method:'POST',body:JSON.stringify({agentId:snapshot.state.agents[0].id,channel:snapshot.state.agents[0].id})});assert(response.ok,await response.text());
assert(snapshot.state.messages.some(m=>m.text==='Anthropic reply'),'saved-key agent must reach its chosen provider and persist a reply');
response=await window.fetch('https://api.openai.com/v1/chat/completions',{method:'POST',headers:{Authorization:'Bearer '},body:'{}'});assert(response.ok);
console.log('Saved-provider agent reply and empty authorization normalization passed.');

const revisit=completeSetup(snapshot.state,{name:'Second',provider:'OpenAI',model:'gpt-4.1-mini',instructions:'Help'},true);
assert.equal(revisit.agents.length,2);assert.equal(revisit.messages.length,snapshot.state.messages.length);assert.equal(revisit.workbench.fs.entries.length,snapshot.state.workbench.fs.entries.length);
const {mergeTerminalFiles}=load('frontend/shared/lib/workbench.ts');
const baseline=structuredClone(snapshot.state.workbench.fs),shellState=structuredClone(snapshot.state);
const guestFiles=baseline.entries.filter(f=>!f.trashed).map(f=>({path:f.path,kind:f.kind,encoding:'base64',content:f.kind==='file'?(f.encoding==='base64'?f.content:btoa(unescape(encodeURIComponent(f.content)))):''}));
guestFiles.push({path:'/workspace/from-terminal.txt',kind:'file',encoding:'base64',content:btoa('made in a PTY')});
mergeTerminalFiles(shellState.workbench,baseline,guestFiles,'terminal-check');
assert(shellState.workbench.fs.entries.some(f=>f.path==='/workspace/from-terminal.txt'&&f.content==='made in a PTY'&&f.encoding==='utf8'));
console.log('Setup replay preserves agents/messages/files; terminal text exports remain editable.');

const conflictState=structuredClone(snapshot.state),conflictBase=structuredClone(conflictState.workbench.fs);
const target=conflictBase.entries.find(f=>f.kind==='file'&&f.encoding==='utf8');
const edited=load('frontend/shared/lib/space.ts').applyCommand(conflictState,'fs_write',{path:target.path,content:'human edit',expectedVersion:target.version});
const exportFiles=conflictBase.entries.filter(f=>!f.trashed).map(f=>({path:f.path,kind:f.kind,encoding:'base64',content:f.kind==='file'?btoa(unescape(encodeURIComponent(f.path===target.path?'terminal edit':f.content))):''}));
const conflicts=mergeTerminalFiles(edited.workbench,conflictBase,exportFiles,'conflict-check');
assert(conflicts.includes(target.path));assert(edited.workbench.fs.entries.some(f=>f.path===target.path&&f.content==='human edit'));assert(edited.workbench.fs.entries.some(f=>f.path.startsWith('/workspace/Conflicts/conflict-check')&&f.content==='terminal edit'));
console.log('Concurrent editor changes preserved; conflicting terminal version recovered.');

const {shouldOfferSetup}=load('frontend/setup-state.ts');assert(shouldOfferSetup(legacy,{}));assert(!shouldOfferSetup(legacy,{OpenAI:true}));assert(shouldOfferSetup(initialSpace(),{OpenAI:true}));
console.log('Provider-less returning workspaces reopen setup without deleting their data.');

const sharedPath='/workspace/Shared/project/note.txt';let physical={id:'native-file',path:sharedPath,kind:'file',content:'on disk',encoding:'utf8',version:40,modified:1,mounted:true,readOnly:false};let readOnly=false;
bindings.SharedFolders=async()=>[{name:'project',path:'/workspace/Shared/project',hostPath:'/Users/private/project',available:true,readOnly}];
bindings.ReadShared=async(path)=>{if(path===sharedPath)return {entry:{...physical,readOnly}};if(path==='/workspace/Shared/project')return {entry:{path,kind:'directory'},entries:[physical]};throw Error('not found')};
bindings.WriteShared=async(path,content,encoding,expected)=>{assert.equal(path,sharedPath);if(readOnly)throw Error('read-only');assert.equal(expected,physical.version);physical={...physical,content,encoding,version:physical.version+1};return {...physical}};
snapshot={state:initialSpace(),revision:0};
let mounted=await command('fs_open',{path:sharedPath});assert.equal(mounted.state.workbench.activeFile,sharedPath);assert(!JSON.stringify(mounted.state).includes('/Users/private/project'));
await command('file_write',{name:sharedPath,content:'editor saved to disk',expectedVersion:40});assert.equal(physical.content,'editor saved to disk');
let projected=await load('frontend/store.ts').readSpace();assert.equal(projected.state.workbench.fs.entries.find(e=>e.path===sharedPath).version,41);
const previousMountRevision=projected.mountRevision;physical={...physical,content:'external edit',version:42};projected=await load('frontend/store.ts').readSpace();assert.notEqual(projected.mountRevision,previousMountRevision);assert.equal(projected.state.workbench.fs.entries.find(e=>e.path===sharedPath).content,'external edit');
await assert.rejects(()=>command('file_write',{name:sharedPath,content:'stale overwrite',expectedVersion:41}),/changed/);assert.equal(physical.content,'external edit');
readOnly=true;await assert.rejects(()=>command('file_write',{name:sharedPath,content:'forbidden',expectedVersion:42}),/read-only/);assert.equal(physical.content,'external edit');
const {readWorkspacePath}=load('frontend/mounted-fs.ts');const mountedRead=await readWorkspacePath(projected.state.workbench.fs,{path:sharedPath});assert.equal(mountedRead.entry.content,'external edit');
console.log('Mounted editor and agent bridge: live saves, external edits, stale-write protection, read-only enforcement, no host-path leakage.');
