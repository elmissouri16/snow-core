/// <reference path="../snow-v2.d.ts" />
snow.registerView({name:"overview",title:"Workspace dashboard",placement:"sidebar"});
snow.registerView({name:"status",title:"Agent status",placement:"footer"});
async function refresh(_,ctx){
 const state=await ctx.agent.state();
 await ctx.ui.update({name:"status",content:{type:"text",text:String(snow.config.label)+" · "+(state.running?"working":"ready"),tone:state.running?"warning":"muted"}});
 await ctx.ui.update({name:"overview",content:{type:"column",children:[
  {type:"text",text:String(snow.config.label),tone:"accent"},
  {type:"text",text:ctx.cwd},
  {type:"text",text:"Model: "+String(state.model.id||"unknown")},
  {type:"text",text:"Mode: "+state.mode},
  {type:"button",text:"Refresh",action:"refresh"}
 ]}});
}
snow.onReady(refresh);
snow.on("turn_done",refresh);
snow.on("model_changed",refresh);
snow.on("session_updated",refresh);
snow.registerCommand({name:"refresh",description:"Refresh workspace dashboard",uses:["ui"],run:refresh});
snow.registerCommand({name:"open",alias:"dashboard",description:"Open workspace dashboard",uses:["ui"],async run(_,ctx){await refresh(_,ctx);await ctx.ui.open({name:"overview"});}});
