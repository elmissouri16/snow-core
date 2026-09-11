/// <reference path="../snow-v2.d.ts" />
snow.registerTool({name:"files",description:"Find project source files",parameters:{type:"object",properties:{},additionalProperties:false},uses:["glob"],child:true,async execute(_,ctx){
 const result=await ctx.tools.call("glob",{pattern:String(snow.config.pattern)});
 return {content:result.content,isError:result.isError,details:{pattern:snow.config.pattern}};
}});
snow.registerToolRenderer("files",result=>({type:"column",children:[{type:"text",text:"Project sources · "+String(result.details.pattern),tone:"accent"},...result.content.map(block=>({type:"text",text:block.text}))]}));
snow.registerCommand({name:"draft",description:"Prepare a review prompt in the composer",uses:["ui"],async run(input,ctx){
 const focus=input||await ctx.ui.input({title:"What should the review focus on?"});
 await ctx.ui.editorSet({text:"Review this project for "+focus+". Inspect the source and tests; report evidence with file references."});
}});
// A pure hook can use cached/local configuration, but cannot call host APIs.
snow.registerHook("before_request",()=>({context:[{text:"Project source pattern configured by project-helper-v2: "+String(snow.config.pattern)}]}));

// Select this package's tool explicitly for an isolated explorer runtime.
snow.registerCommand({name:"scout",alias:"source-scout",description:"Give one explorer the project files plugin tool",argumentHint:"[question]",uses:["subagents","ui"],async run(input,ctx){
 const tool="plugin_project-helper-v2_files";
 const child=await ctx.subagents.spawn({name:"scout_"+Date.now().toString(36),role:"plugin_scout",fork_turns:"none",plugin_tools:[tool],task:
  "Use "+tool+" to discover source files, then inspect relevant files with read/grep. Do not modify files. Answer with file references: "+(input.trim()||"Where does the main agent loop live?")});
 await ctx.ui.notify({text:"Source scout started with its own plugin runtime"});
 let state=child;
 while(!["completed","errored","interrupted","shutdown","closed","not_loaded"].includes(state.status)){
  await ctx.sleep(500);
  state=await ctx.subagents.get({target:child.agent.path});
 }
 await ctx.subagents.close({target:child.agent.path});
 return {content:[{type:"text",text:state.result||state.error||("Scout finished: "+state.status)}],isError:state.status!=="completed"};
}});
