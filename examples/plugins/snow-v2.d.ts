/** Snow JavaScript API 2. Promise APIs run in Goja; Node globals are unavailable. */
type JSONValue = null | boolean | number | string | JSONValue[] | { [key: string]: JSONValue };
type Params = Record<string, unknown>;
interface SnowResult { content: {type: "text"; text: string}[]; isError?: boolean; details?: JSONValue; }
interface SnowNode {
 type: "text"|"markdown"|"row"|"column"|"list"|"table"|"progress"|"button"|"input"|"select"|"checkbox";
 id?: string; text?: string; tone?: "accent"|"muted"|"warning"|"error"|"success";
 children?: SnowNode[]; columns?: string[]; rows?: string[][]; value?: number; action?: string; input?: string;
}
interface SnowSetting {name:string; title:string; type:"string"|"number"|"boolean"|"enum"; default?:JSONValue; choices?:string[];}
interface SnowAgentState {running:boolean; sessionId:string; model:Params; mode:string; thinking:string; usage:Params;}
interface SnowChild {agent:{path:string; thread_id:string; role:string}; status:string; result?:string; error?:string; plugin_tools?:Record<string,string>;}
interface SnowContext {
 readonly sessionId:string; readonly cwd:string; readonly kind:"root"|"child"; readonly toolCallId:string;
 progress(message:string):void; sleep(milliseconds:number):Promise<void>;
 agent:{state():Promise<SnowAgentState>; prompt(args:{text:string}):Promise<void>; steer(args:{text:string}):Promise<void>; followUp(args:{text:string}):Promise<void>; pending():Promise<unknown>; abort():Promise<void>};
 models:{list():Promise<{provider:string; model:Params; models:Params[]}>; set(args:{provider?:string;model?:string;thinking?:string}):Promise<void>};
 session:{messages(args?:{offset?:number;limit?:number}):Promise<Params[]>; branches():Promise<Params[]>; rename(args:{name:string}):Promise<void>; fork(args?:Params):Promise<Params>; selectBranch(args:{id:string}):Promise<{scheduled:boolean}>; renameBranch(args:{id:string;name:string}):Promise<Params>; deleteBranch(args:{id:string}):Promise<void>; compact():Promise<Params>};
 goals:{get():Promise<unknown>; create(args:{objective:string;budget?:number;replace?:boolean}):Promise<unknown>; edit(args:{objective:string}):Promise<unknown>; pause():Promise<unknown>; resume():Promise<unknown>; clear():Promise<void>};
 subagents:{models():Promise<Params[]>; spawn(args:{name:string;task:string;role?:string;fork_turns?:string;provider?:string;model?:string;reasoning_effort?:string;plugin_tools?:string[]}):Promise<SnowChild>; list(args?:{target?:string}):Promise<{agents:SnowChild[];running:number;queued:number;terminal:number}>; get(args:{target:string}):Promise<SnowChild>; messages(args:{target:string;offset?:number;limit?:number}):Promise<Params[]>; message(args:{target:string;text:string}):Promise<void>; followUp(args:{target:string;text:string}):Promise<void>; wait(args?:{timeoutMS?:number;until?:"all"|"any"}):Promise<Params>; interrupt(args:{target:string}):Promise<string>; close(args:{target:string}):Promise<string>; resume(args:{target:string}):Promise<SnowChild>};
 tools:{call(name:string,args?:Params):Promise<SnowResult>};
 storage:{get(args:{scope?:"global"|"project"|"session";key:string}):Promise<JSONValue>; set(args:{scope?:"global"|"project"|"session";key:string;value:JSONValue}):Promise<void>; delete(args:{scope?:"global"|"project"|"session";key:string}):Promise<void>};
 ui:{readonly available:boolean; update(args:{name:string;content:SnowNode}):Promise<void>; open(args:{name:string}):Promise<void>; close():Promise<void>; notify(args:{text:string}):Promise<void>; input(args:{title:string}):Promise<string>; select(args:{title:string;options:string[]}):Promise<string>; confirm(args:{title:string}):Promise<boolean>; form(args:{title:string;fields:SnowSetting[]}):Promise<Record<string,JSONValue>>; editorGet():Promise<string>; editorSet(args:{text:string}):Promise<void>; editorInsert(args:{text:string}):Promise<void>; theme(args:{name:string}):Promise<void>};
}
interface SnowHook {phase:"before_prompt"|"before_request"|"before_tool"|"after_tool";text?:string;tool?:string;arguments?:Params;content?:{type:"text";text:string}[];isError?:boolean;context?:{source?:string;text:string}[];agent?:Params;}
interface SnowHookResult {text?:string;arguments?:Params;content?:{type:"text";text:string}[];context?:{text:string}[];block?:string;}
declare const snow:{
 readonly config:Record<string,JSONValue>; readonly runtime:{apiVersion:2;kind:"root"|"child"};
 registerTool(def:{name:string;description:string;parameters:Params;uses?:string[];child?:boolean;execute(args:Params,ctx:SnowContext):SnowResult|string|Promise<SnowResult|string>}):void;
 registerCommand(def:{name:string;description:string;argumentHint?:string;alias?:string;shortcut?:string;uses?:string[];timeoutMS?:number;run(input:string,ctx:SnowContext):void|string|SnowResult|Promise<void|string|SnowResult>}):void;
 registerHook(phase:SnowHook["phase"],handler:(request:SnowHook,ctx:SnowContext)=>SnowHookResult|Promise<SnowHookResult>,options?:{includeSubagents?:boolean}):void;
 registerView(def:{name:string;title:string;placement:"header"|"footer"|"sidebar"|"above_input"|"screen"}):void;
 registerTheme(def:{name:string;colors:Record<"accent"|"muted"|"foreground"|"warning"|"error"|"success"|"separator",{light:string;dark:string}>}):void;
 registerToolRenderer(tool:string,handler:(result:SnowResult)=>SnowNode):void;
 onReady(handler:(event:Params,ctx:SnowContext)=>void|Promise<void>):void;
 on(type:string,handler:(event:{version:number;type:string;payload:Params},ctx:SnowContext)=>void|Promise<void>):void;
 onClose(handler:()=>void):void;
 log(level:"info"|"warning"|"error",message:string):void;
};
