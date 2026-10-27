import fs from 'node:fs';import path from 'node:path';import vm from 'node:vm';import assert from 'node:assert/strict';
import ts from '../frontend/node_modules/typescript/lib/typescript.js';
const cache=new Map();let snapshot={state:null,revision:-1};
const bindings={async SandboxStatus(){return {available:true}},async ReadWorkspace(){return structuredClone(snapshot)},async CommitWorkspace(revision,json){await new Promise(r=>setTimeout(r,1));if(revision!==snapshot.revision)throw Error('Revision conflict; retry');snapshot={state:JSON.parse(json),revision:revision+1};return structuredClone(snapshot)}};
const window={go:{main:{App:bindings}},fetch:async()=>{throw Error('Unexpected network call')}};
const globals={window,location:{href:'http://wails.localhost/',origin:'http://wails.localhost'},crypto,structuredClone,TextEncoder,Uint8Array,Date,atob,btoa,Response,Request,URL,AbortSignal,DOMException,setTimeout};
function load(file){file=path.resolve(file);if(cache.has(file))return cache.get(file).exports;const module={exports:{}};cache.set(file,module);const output=ts.transpileModule(fs.readFileSync(file,'utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText;vm.runInNewContext('(function(require,module,exports){'+output+'\n})',globals)(p=>p==='cloudflare:workers'?{env:{}}:load(path.resolve(path.dirname(file),p+'.ts')),module,module.exports);return module.exports}
load('frontend/transport.ts').installTransport();
let response=await window.fetch('/api/space');assert(response.ok);let data=await response.json();assert.equal(data.revision,0);assert(data.state.workbench.fs.entries.length>0);
const act=(name,args)=>window.fetch('/api/space',{method:'POST',body:JSON.stringify({name,args}),headers:{'Content-Type':'application/json'}});
response=await act('fs_write',{path:'/workspace/native-check.txt',content:'Saved through Go contract',expectedVersion:0});assert(response.ok);data=await response.json();assert.equal(data.revision,1);
response=await window.fetch('/api/space');data=await response.json();assert(data.state.workbench.fs.entries.some(e=>e.path==='/workspace/native-check.txt'&&e.content==='Saved through Go contract'));
const parallel=await Promise.all([act('window_control',{window:'code',action:'show'}),act('window_control',{window:'writer',action:'show'})]);assert(parallel.every(r=>r.ok));assert.equal(snapshot.revision,3);
response=await act('sandbox_shell',{command:'pwd'});assert(response.ok);data=await response.json();assert.equal(data.state.workbench.runs[0].executionBackend,'apple-vz');assert.equal(data.state.workbench.runs[0].sandboxActor,'you');await act('notebook_cancel',{});
response=await act('unknown',{});assert.equal(response.status,400);
response=await window.fetch('/api/run',{method:'POST',body:JSON.stringify({agentId:'milo'})});assert.equal(response.status,409);
response=await window.fetch('/api/colab',{method:'POST',body:'{}'});assert.equal(response.status,409);
console.log('Desktop transport: initialization, persistence contract, concurrent saves, validation and unavailable-provider errors passed.');
