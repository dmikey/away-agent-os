export type AgentCursor={x:number;y:number;action:string;activeUntil?:number;runToken?:string};
export const CURSOR_ACTIVITY_MS=10000;
export const CURSOR_RUN_MS=60000;
export function cursorIsActive(cursor:AgentCursor,now=Date.now()){return Number.isFinite(cursor.activeUntil)&&cursor.activeUntil!>now;}
