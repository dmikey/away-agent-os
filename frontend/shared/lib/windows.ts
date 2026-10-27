export type WindowState={open:boolean;x:number;y:number;width?:number;height?:number;minimized?:boolean;hidden?:boolean};
export type WindowArgs=Record<string,unknown>;
// Shared by the authoritative command reducer and immediate UI projection.
export function changeWindow(current:WindowState,args:WindowArgs):WindowState{
 const w={...current},action=args.action;
 if(action!==undefined&&!['open','show','minimize','hide','close'].includes(String(action)))throw Error('Unknown window action.');
 if(action==='open'||action==='show'){w.open=true;w.minimized=false;w.hidden=false}
 if(action==='minimize'){w.open=true;w.minimized=true;w.hidden=false}
 if(action==='hide')w.hidden=true;
 if(action==='close'){w.open=false;w.minimized=false;w.hidden=false}
 if(typeof args.open==='boolean'){w.open=args.open;w.minimized=false;w.hidden=false}
 for(const key of ['x','y'] as const)if(Number.isFinite(args[key]))w[key]=Math.min(10000,Math.max(-10000,Number(args[key])));
 if(Number.isFinite(args.width))w.width=Math.min(2400,Math.max(240,Number(args.width)));
 if(Number.isFinite(args.height))w.height=Math.min(1600,Math.max(150,Number(args.height)));
 return w;
}
export class WindowIntents{
 private sequence=0;
 private entries:{token:number;args:WindowArgs}[]=[];
 add(args:WindowArgs){const token=++this.sequence;this.entries.push({token,args:{...args}});return token}
 settle(token:number){this.entries=this.entries.filter(entry=>entry.token!==token)}
 project(windows:Record<string,WindowState>){if(!this.entries.length)return windows;const next={...windows};for(const {args} of this.entries){const id=String(args.window);if(next[id])next[id]=changeWindow(next[id],args)}return next}
}
