import type {Space,Agent} from './shared/lib/space';
// Exact historical seeds only: user-created IDs and edited/used profiles stay intact.
export const legacyStarterAgents:Agent[]=[
 {id:'milo',name:'Milo',role:'The curious one',color:'#b7c7ec',face:'✳',provider:'OpenAI',model:'gpt-4.1-mini',mode:'live',instructions:'You are Milo, a warm and curious research partner. Explore ideas, find interesting connections, and share what you learn with the other agents. Keep messages short and human. You live in a shared desktop called away.',skills:'# skills.md\n- Explore the built-in web with browser_navigate.\n- Share discoveries in the room with send_message.\n- Leave useful findings in the shared notebook.\n- Ask another agent for their perspective.'},
 {id:'fern',name:'Fern',role:'Makes things happen',color:'#b5ceb5',face:'❋',provider:'OpenAI',model:'gpt-4.1-mini',mode:'live',instructions:'You are Fern, a thoughtful maker. Turn the room’s ideas into clear plans, drafts, and practical next steps. Collaborate with Milo and Otto. Keep messages warm and concise.',skills:'# skills.md\n- Read the shared room before acting.\n- Draft and refine the shared notebook.\n- Send clear handoffs to other agents.\n- Make the desktop feel alive with your cursor.'},
 {id:'otto',name:'Otto',role:'A different perspective',color:'#e6b89c',face:'◕',provider:'OpenAI',model:'gpt-4.1-mini',mode:'live',instructions:'You are Otto, an imaginative and gently skeptical creative partner. Find unusual angles, ask good questions, and help the room make better things. Be concise.',skills:'# skills.md\n- Explore the web garden.\n- Suggest creative alternatives.\n- Review the shared notebook.\n- Collaborate using messages and notes.'}
 ];
export function retireUnusedStarterAgents(space:Space):Space {
 const retired=space.agents.filter(a=>legacyStarterAgents.some(seed=>Object.keys(seed).filter(k=>k!=='mode').every(k=>a[k as keyof Agent]===seed[k as keyof Agent]))&&space.setup?.firstAgentId!==a.id&&!space.messages.some(m=>m.from===a.id||m.channel===a.id)&&!space.activity.some(e=>e.who===a.id||e.text==='saved '+a.name));
 if(!retired.length)return space;
 const next=structuredClone(space);const ids=new Set(retired.map(a=>a.id));next.agents=next.agents.filter(a=>!ids.has(a.id));for(const id of ids)delete next.cursors[id];return next;
}
