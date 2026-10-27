import {defineConfig} from 'vite';
import react from '@vitejs/plugin-react';
import {fileURLToPath} from 'node:url';
import {GET} from './shared/app/web/[slug]/route';
export default defineConfig({base:'./',plugins:[react(),{
 name:'away-built-in-pages',async generateBundle(){for(const slug of ['start','garden','manifesto','radio']){const response=await GET(new Request('https://away.local/web/'+slug),{params:Promise.resolve({slug})});this.emitFile({type:'asset',fileName:'web/'+slug+'/index.html',source:await response.text()})}}
}],publicDir:'shared/public',resolve:{alias:[
 {find:/^(.*\/)?lib\/store$/,replacement:fileURLToPath(new URL('./store.ts',import.meta.url))},
 {find:'cloudflare:workers',replacement:fileURLToPath(new URL('./env.ts',import.meta.url))}
]}});
