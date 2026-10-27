'use client';
import {useEffect,useState} from 'react';
import {MousePointer2} from 'lucide-react';
import {cursorIsActive} from '../../lib/cursor';
import type {Space} from '../../lib/space';
export function AgentCursors({cursors,agents}:{cursors:Space['cursors'];agents:Space['agents']}){
 const [now,setNow]=useState(Date.now);const hasActive=Object.values(cursors).some(c=>cursorIsActive(c));
 useEffect(()=>{setNow(Date.now());if(!hasActive)return;const timer=setInterval(()=>setNow(Date.now()),500);return()=>clearInterval(timer)},[cursors,hasActive]);
 return <>{Object.entries(cursors).filter(([,c])=>cursorIsActive(c,Math.max(now,Date.now()))).map(([id,c])=>{const a=agents.find(a=>a.id===id);if(!a)return null;return <div key={id} data-agent-cursor={id} className="agent-cursor" style={{left:c.x+'%',top:c.y+'%',color:a.color}}><MousePointer2 size={23} fill={a.color} stroke="#344443" strokeWidth={1.4}/><span style={{background:a.color}}>{a.name}<small>{c.action}</small></span></div>})}</>;
}
