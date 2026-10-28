// Explicit live-model check; credentials stay in the native helper's Keychain bridge.
import fs from 'node:fs';import vm from 'node:vm';import assert from 'node:assert/strict';import {spawnSync} from 'node:child_process';
import ts from '../frontend/node_modules/typescript/lib/typescript.js';
const helper=process.argv[2];if(!helper)throw Error('Pass the signed cmd/agentcheck helper path. This makes three provider API calls.');
const cases=[
 {text:'Can you checkout test.playmos.io ?',expected:'test.playmos.io'},
 {text:'For our notes, quote this sentence: "Donald cannot inspect test.playmos.io". Do not browse any website.',expected:null},
 {text:'Hi Donald, how are you?',expected:null}
];
// Use the actual planner function and its forced-tool request contract.
// The sandboxed module resolves this facade rather than a raw API credential.
for(const c of cases){
 const agent={provider:'OpenAI',model:'gpt-4.1-mini'};
 const source=ts.transpileModule(fs.readFileSync('frontend/agent-task.ts','utf8'),{compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}}).outputText;
 const module={exports:{}};vm.runInNewContext('(function(require,module,exports){'+source+'})',{URL,AbortSignal,structuredClone,fetch:async(url,init)=>{const output=spawnSync(helper,['-live'],{input:init.body,encoding:'utf8',timeout:60000,maxBuffer:1024*1024});if(output.status!==0)throw Error(output.stderr.trim()||'Native model check failed');return {ok:true,json:async()=>JSON.parse(output.stdout)}}})(()=>({providerInfo:()=>({endpoint:'https://api.openai.com/v1/chat/completions'})}),module,module.exports);
 const result=await module.exports.prepareAgentTask(agent,'',c.text,new AbortController().signal);
 if(c.expected){assert.equal(result.requests.length,1);assert.equal(new URL(result.requests[0].url).hostname,c.expected);assert(result.requests[0].reason)}else assert.equal(result.requests.length,0);
 console.log('LIVE_MODEL_PREFLIGHT_OK:',c.expected?'requests permission for '+c.expected:'no access request for '+(c.text.startsWith('Hi')?'a greeting':'a quoted URL / do-not-browse instruction'));
}
