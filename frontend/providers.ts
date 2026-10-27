export const providers=[
 {id:'OpenAI',family:'GPT',mark:'◉',description:'A versatile thinking partner.',keysUrl:'https://platform.openai.com/api-keys',endpoint:'https://api.openai.com/v1/chat/completions'},
 {id:'Anthropic',family:'Claude',mark:'✳',description:'A thoughtful collaborator.',keysUrl:'https://platform.claude.com/settings/keys',endpoint:'https://api.anthropic.com/v1/chat/completions'},
 {id:'xAI',family:'Grok',mark:'𝕏',description:'A curious new perspective.',keysUrl:'https://console.x.ai/team/default/api-keys',endpoint:'https://api.x.ai/v1/chat/completions'},
] as const;
export type ProviderName=typeof providers[number]['id'];
export function providerInfo(name:string){const p=providers.find(p=>p.id===name);if(!p)throw Error('Choose a supported provider.');return p}
export function chooseModel(models:string[]){return models.find(m=>m==='gpt-4.1-mini')||models.find(m=>/^claude-sonnet/.test(m))||models.find(m=>/^grok-.*fast/.test(m))||models[0]||''}
