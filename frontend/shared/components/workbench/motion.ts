'use client';
import {useEffect,useRef,useState} from 'react';
type Windows=Record<string,{open:boolean;hidden?:boolean;minimized?:boolean}>;
const visible=(w:Windows[string])=>w.open&&!w.hidden&&!w.minimized;
export function useWindowMotion(windows:Windows){
 const [present,setPresent]=useState(()=>Object.keys(windows).filter(id=>visible(windows[id])));const timers=useRef(new Map<string,ReturnType<typeof setTimeout>>());
 useEffect(()=>{for(const [id,w] of Object.entries(windows)){if(visible(w)){const timer=timers.current.get(id);if(timer){clearTimeout(timer);timers.current.delete(id)}setPresent(p=>p.includes(id)?p:[...p,id]);}else if(present.includes(id)&&!timers.current.has(id)){const delay=window.matchMedia('(prefers-reduced-motion: reduce)').matches?0:160;timers.current.set(id,setTimeout(()=>{setPresent(p=>p.filter(k=>k!==id));timers.current.delete(id)},delay));}}},[windows,present]);
 useEffect(()=>()=>{for(const timer of timers.current.values())clearTimeout(timer)},[]);
 return {has:(id:string)=>present.includes(id),className:(id:string)=>visible(windows[id])?'':' window-leaving '+(windows[id].minimized?'window-minimizing':windows[id].hidden?'window-hiding':'window-closing')};
}
