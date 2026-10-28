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

const {retireUnusedStarterAgents,legacyStarterAgents}=load('frontend/legacy-agents.ts');
const oldSeeded=initialSpace();oldSeeded.agents=structuredClone(legacyStarterAgents);delete oldSeeded.setup;
assert.equal(retireUnusedStarterAgents(oldSeeded).agents.length,0);
const customized=structuredClone(oldSeeded);customized.agents[0].instructions+=' User changes.';customized.messages=[{id:'history',from:'you',channel:'fern',text:'Hello',at:1}];customized.agents.push({...legacyStarterAgents[2],id:'user-created-otto'});
assert.deepEqual(Array.from(retireUnusedStarterAgents(customized).agents,a=>a.id),['milo','fern','user-created-otto']);
const chosen=structuredClone(oldSeeded);chosen.setup={version:1,completed:true,firstAgentId:'otto'};assert.equal(retireUnusedStarterAgents(chosen).agents[0].id,'otto');
const historical=structuredClone(oldSeeded);historical.activity.push({id:'edited',who:'you',text:'saved Otto',at:1});assert.equal(retireUnusedStarterAgents(historical).agents[0].id,'otto');
snapshot={state:oldSeeded,revision:0};bindings.SharedFolders=async()=>[];
await load('frontend/store.ts').readSpace();assert.equal(snapshot.state.agents.length,0);assert.equal(snapshot.revision,1);
const migrated=await command('setup_complete',{agent:{name:'My agent',provider:'Anthropic',model:'test',instructions:'Help'},revisit:true});assert.equal(migrated.state.agents.length,1);
const chosenId=migrated.state.agents[0].id;const storedMessages=structuredClone(snapshot.state.messages),storedFiles=JSON.stringify(snapshot.state.workbench.fs);
const removed=await act('agent_delete',{agentId:chosenId});assert(removed.ok);assert.equal(snapshot.state.agents.length,0);assert.equal(JSON.stringify(snapshot.state.workbench.fs),storedFiles);assert.deepEqual(snapshot.state.messages,storedMessages);assert.equal(snapshot.state.setup.completed,true);assert(!snapshot.state.setup.firstAgentId);assert(connections.Anthropic);
assert.equal((await act('agent_delete',{agentId:'missing'})).status,400);
console.log('Legacy starter migration retires only untouched unused seeds; custom, used, saved and chosen agents survive. Human removal preserves messages, files, setup and provider connection.');

// Real agent route + provider transport contract, scripted model responses (no API calls).
snapshot={state:initialSpace(),revision:0};connections.OpenAI=true;
await command('agent_save',{agent:{name:'Writer agent',provider:'OpenAI',model:'test',instructions:'Write stories.'}});
const writingAgent=snapshot.state.agents[0].id;
let writerState=(await load('frontend/store.ts').readSpace()).state;
const originalDocument=structuredClone(writerState.workbench.document);assert.equal(originalDocument.version,1);
await command('window_control',{window:'writer',action:'show'});await command('window_control',{window:'writer',action:'minimize'});
let modelStep=0,createdPath='';
const tool=(name,args)=>({choices:[{message:{role:'assistant',content:null,tool_calls:[{id:'step-'+modelStep,type:'function',function:{name,arguments:JSON.stringify(args)}}]}}]});
bindings.CompleteProvider=async(id,provider,key,body)=>{const req=JSON.parse(body);assert(req.messages[0].content.includes('mode:create'));let result;
 if(modelStep===0)result=tool('window_control',{window:'writer',action:'show'});
 else if(modelStep===1)result=tool('space_read',{});
 else if(modelStep===2){const read=JSON.parse(req.messages.at(-1).content);assert.equal(read.state.workbench.document.version,1);assert(!snapshot.state.windows.writer.minimized);result=tool('document_write',{mode:'create',title:'The Moon Garden',html:'<h1>The Moon Garden</h1><p>The gate opened.</p>',expectedVersion:0})}
 else if(modelStep===3){const saved=JSON.parse(req.messages.at(-1).content);createdPath=saved.state.workbench.document.path;assert.equal(saved.state.workbench.document.version,1);assert.equal(saved.state.presentation.window,'writer');assert.equal(saved.state.presentation.actor,writingAgent);result=tool('space_read',{})}
 else if(modelStep===4){const current=JSON.parse(req.messages.at(-1).content).state.workbench.document;result=tool('document_write',{mode:'update',path:createdPath,html:'<h1>The Moon Garden</h1><p>The gate opened.</p><p>Marv found the stars. The end.</p>',expectedVersion:current.version})}
 else {const saved=JSON.parse(req.messages.at(-1).content);assert.equal(saved.state.workbench.document.version,2);result={choices:[{message:{role:'assistant',content:'Saved the story in Writer: '+createdPath}}]}}
 modelStep++;return {status:200,body:JSON.stringify(result)};
};
response=await window.fetch('/api/run',{method:'POST',body:JSON.stringify({agentId:writingAgent,channel:writingAgent})});assert(response.ok,await response.text());assert.equal(modelStep,6);
assert(snapshot.state.workbench.document.html.includes('The end.'));assert(snapshot.state.windows.writer.open&&!snapshot.state.windows.writer.hidden&&!snapshot.state.windows.writer.minimized);
assert.equal(snapshot.state.workbench.fs.entries.find(e=>e.path===originalDocument.path).content,originalDocument.html);
const secondStory=await command('document_write',{mode:'create',title:'The Moon Garden',html:'<p>Another story</p>',expectedVersion:0});assert.notEqual(secondStory.state.workbench.document.path,createdPath);assert(secondStory.state.workbench.fs.entries.find(e=>e.path===createdPath).content.includes('The end.'));
await assert.rejects(()=>command('document_write',{mode:'create',path:createdPath,html:'<p>Must not overwrite</p>',expectedVersion:0}),/already exists/);
await assert.rejects(()=>command('document_write',{mode:'update',path:createdPath,html:'<p>Stale</p>',expectedVersion:1}),/changed/);
const legacyCreate=await command('document_write',{title:'A different story',html:'<p>New work</p>',expectedVersion:0});assert.equal(legacyCreate.state.workbench.document.title,'A different story');
// A concurrent window save must not corrupt the rename's expected version on retry.
const commitNormally=bindings.CommitWorkspace;let failNextRename=true;
bindings.CommitWorkspace=async(revision,json)=>{if(failNextRename){failNextRename=false;snapshot={...snapshot,revision:snapshot.revision+1};throw Error('Revision conflict; retry')}return commitNormally(revision,json)};
const renamed=await command('document_write',{mode:'update',title:'Renamed story',html:'<p>Kept story</p>',expectedVersion:legacyCreate.state.workbench.document.version});assert.equal(renamed.state.workbench.document.title,'Renamed story');bindings.CommitWorkspace=commitNormally;
console.log('Writer workflow: six-step agent opens/restores Writer, creates and finishes a separate story, foregrounds it, and preserves existing documents. Duplicate titles are safe, stale writes rejected, unrelated window conflicts retry without false document conflicts.');

let recoveryStep=0;
bindings.CompleteProvider=async(id,provider,key,body)=>{const req=JSON.parse(body);let result;
 if(recoveryStep===0)result=tool('document_write',{mode:'update',path:originalDocument.path,html:'<p>Wrong version must not overwrite</p>',expectedVersion:0});
 else if(recoveryStep===1){const failure=JSON.parse(req.messages.at(-1).content);assert.equal(failure.code,'DOCUMENT_VERSION_MISMATCH');assert(failure.recovery.includes('mode:create'));result=tool('space_read',{})}
 else if(recoveryStep===2)result=tool('document_write',{mode:'create',title:'Recovered draft',html:'<p>Saved without disturbing existing work.</p>',expectedVersion:0});
 else result={choices:[{message:{role:'assistant',content:'Saved a separate draft in Writer.'}}]};recoveryStep++;return {status:200,body:JSON.stringify(result)};
};
response=await window.fetch('/api/run',{method:'POST',body:JSON.stringify({agentId:writingAgent,channel:writingAgent})});assert(response.ok,await response.text());assert.equal(recoveryStep,4);assert.equal(snapshot.state.workbench.fs.entries.find(e=>e.path===originalDocument.path).content,originalDocument.html);
console.log('Writer tool conflicts return current metadata and safe recovery instructions; a retry can create a separate draft without asking the user or overwriting existing work.');
