import {applyCommand,type Space,type Agent} from './shared/lib/space';
import {providerInfo} from './providers';
export const needsSetup=(space:Space)=>space.setup?.completed===false;
// One revision commits the identity, completion marker, and calm first desktop together.
export function completeSetup(space:Space,agent:Agent,revisit=false):Space{
 if(!needsSetup(space)&&!revisit)throw Error('First-time setup is already complete. Edit your agent in Messenger settings.');
 providerInfo(agent.provider);
 if(!agent.name?.trim()||!agent.model?.trim()||!agent.instructions?.trim())throw Error('Give your agent a name, model, and working instructions.');
 const next=applyCommand(space,'agent_save',{agent});const first=next.agents[next.agents.length-1];
 next.presentation={window:'chat',token:crypto.randomUUID(),actor:'you'};
 next.setup={version:1,completed:true,provider:first.provider,firstAgentId:first.id};
 next.windows=Object.fromEntries(Object.entries(next.windows).map(([id,w])=>[id,{...w,open:id==='chat',minimized:false,hidden:false}]));
 return next;
}

export const shouldOfferSetup=(space:Space,connections:Record<string,boolean>)=>needsSetup(space)||!Object.values(connections).some(Boolean);
