'use client';
import {useMemo,useState} from 'react';
import {Search,ChevronRight} from 'lucide-react';
import {toolDefinitions} from '../../lib/space';
import {frontendMappings} from './frontend-mcp';
type Tool={name:string;description:string;inputSchema:any};
const extras:Tool[]=[
 {...toolDefinitions.find(t=>t.name==='browser_navigate')!,name:'away_browser_navigate'},
 {...toolDefinitions.find(t=>t.name==='browser_action')!,name:'away_browser_action'},
 {...toolDefinitions.find(t=>t.name==='network_request')!,name:'away_network_request'},
 {...toolDefinitions.find(t=>t.name==='web_fetch')!,name:'away_web_fetch'},
 {name:'away_help',description:'Read the agent handbook, workflows, examples, and runtime limits.',inputSchema:{properties:{}}},
 {name:'away_read',description:'Read the desktop, messages, saved artifacts, and focused window. Provider keys are excluded.',inputSchema:{properties:{includeMedia:{type:'boolean'}}}},
 {name:'away_run_status',description:'Read a guest execution run and its output by run ID.',inputSchema:{properties:{runId:{type:'string'}},required:['runId']}},
 {name:'away_agent_save',description:'Create or edit an agent’s identity, model, instructions, and skills. Does not accept provider keys.',inputSchema:{properties:{agent:{type:'object'}},required:['agent']}},
 {name:'away_export',description:'Download a saved file, notebook, or safe HTML document.',inputSchema:{properties:{kind:{type:'string',enum:['file','notebook','document']},name:{type:'string'}},required:['kind']}}
];
export const frontendTools:Tool[]=[...extras,{...toolDefinitions.find(t=>t.name==='fs_read')!,name:'away_fs_read'},...Object.entries(frontendMappings).map(([name,action])=>({...toolDefinitions.find(t=>t.name===action)!,name}))];
export function ToolCatalog({browserStatus}:{browserStatus:string}){
 const [transport,setTransport]=useState<'site'|'browser'>('site'),[query,setQuery]=useState('');
 const source=transport==='site'?toolDefinitions:frontendTools;
 const tools=useMemo(()=>source.filter(t=>(t.name+' '+t.description).toLowerCase().includes(query.toLowerCase())),[source,query]);
 return <section className="tool-catalog" aria-label="Agent tool catalog"><div className="tool-catalog-heading"><h3>Tool catalog</h3><span>{tools.length} of {source.length}</span></div><div className="tool-catalog-controls"><div role="tablist" aria-label="Tool connection"><button role="tab" aria-selected={transport==='site'} onClick={()=>setTransport('site')}>Agent tools · {toolDefinitions.length}</button><button role="tab" aria-selected={transport==='browser'} onClick={()=>setTransport('browser')}>Browser · {frontendTools.length}</button></div><label><Search size={13}/><input aria-label="Search agent tools" value={query} onChange={e=>setQuery(e.target.value)} placeholder="Find a tool…"/></label></div><p className="tool-catalog-status">{transport==='site'?'Available to built-in agents through the native command broker.':browserStatus+'. Browser tools require this page to stay open.'}</p><div className="tool-catalog-list">{tools.length?tools.map(tool=>{const properties=Object.entries(tool.inputSchema?.properties||{});const run=/file_run|notebook_run|run_status/.test(tool.name);return <details className="tool-catalog-entry" key={tool.name}><summary><ChevronRight size={12}/><code>{tool.name}</code><span>{run?'Python runtime':/read|help|guide|status/.test(tool.name)?'Read':'Workspace action'}</span></summary><p>{tool.description}</p><div className="tool-catalog-parameters">{properties.length?properties.map(([name,schema])=><div key={name}><code>{name}{tool.inputSchema.required?.includes(name)?' *':''}</code><span>{(schema as any).type||'value'}{(schema as any).enum?' · '+(schema as any).enum.join(', '):''}</span></div>):<small>No parameters</small>}</div>{run&&<small className="tool-runtime-note">Execution needs an active supported kernel. A tool being listed does not mean a runtime is connected.</small>}</details>}):<div className="tool-catalog-empty">No tools match “{query}”.</div>}</div></section>;
}
