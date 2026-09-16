import test from "node:test";
import assert from "node:assert/strict";
import {readFileSync} from "node:fs";
import vm from "node:vm";

const app = readFileSync(new URL("../../internal/web/static/app.js", import.meta.url), "utf8");
const snapshot = () => ({project_id:"p", instance_id:"next", session_id:"target", revision:8, status:"idle", messages:[{id:"m",role:"assistant",text:"Saved answer"}]});
function actionHarness() {
  const current = {project:"p", instance:"old", session:"source", key:"p:source", revision:7, status:"idle", connected:true, controller:new AbortController()};
  const region = {dataset:{}}, prompt = {value:"Source draft"};
  const frames = [], posts = [], drafts = new Map([["p:target", "Target draft"]]);
  let resolve;
  const response = new Promise(done => {resolve=done;});
  const context = vm.createContext({live:current, window:{SnowLiveView:{updateDraft(text){prompt.value=text;return true;}}}, AbortController, drafts, uncertain:new Map(), editDrafts:new Map(), reuseDrafts:new Map(), pollTimer:0,
    $: selector => selector === "#managed-processes" ? null : selector === "#live-session" ? region : selector === "#live-prompt" ? prompt : {replaceChildren(){},value:""},
    history:{state:null,replaceState(){}}, projectLocation:()=>"/", document:{},
    mutationSafe:()=>true, goalBlocksHistory:()=>false, settingsAction:()=>false, setupComposerContext(){}, setupQueue(){}, setupVersions(){}, setupGoals(){}, setupLivePanels(){}, disposeLivePanels(){},
    updateControls:()=>frames.push({connected:current.connected,session:current.session,action:current.action,unknown:current.unknown}),
    applySnapshot: result => {current.snapshot=result;current.connected=true;current.status=result.status;current.revision=result.revision;},
    csrf:()=>"csrf", runtimeURL:action=>action, request:(action,fields)=>{posts.push({action,fields});return response;},
    saveDraft:()=>drafts.set(current.key,prompt.value), liveError(){}, clearTimeout(){}, setTimeout:()=>1, startUpdates:()=>frames.push({subscribed:current.instance, connected:current.connected})
  });
  vm.runInContext(app.slice(app.indexOf("  async function runtimeAction("),app.indexOf("  async function cancelTurn(")),context);
  return {current,context,frames,posts,drafts,prompt,resolve,act:()=>context.runtimeAction("switch",{session_id:"target"})};
}

test("verified switch keeps connected history and target draft through subscription without a synchronizing frame", async () => {
  const h=actionHarness(), pending=h.act();
  assert.equal(h.current.action,true); assert.equal(h.posts.length,1);
  assert.equal(h.prompt.value,"Source draft");
  h.resolve(snapshot()); assert.ok(await pending);
  assert.equal(h.current.instance,"next"); assert.equal(h.current.session,"target");
  assert.equal(h.prompt.value,"Target draft"); assert.equal(h.drafts.get("p:source"),"Source draft");
  assert.equal(h.current.snapshot.messages[0].text,"Saved answer");
  assert.ok(h.frames.every(frame=>frame.connected)); assert.equal(h.frames.at(-1).subscribed,"next");
  assert.equal(h.posts.length,1);
});
for (const invalid of ["revision", "messages", "status", "session_id", "instance_id", "permission", "cancel_requested"]) test(`switch rejects invalid ${invalid} instead of making an incomplete result look ready`, async () => {
  const h=actionHarness(), pending=h.act(), result=snapshot();
  if(invalid === "revision") result.revision=-1;
  else if(invalid === "messages") delete result.messages;
  else if(invalid === "permission") result.permission={id:"approval"};
  else if(invalid === "cancel_requested") result.cancel_requested=true;
  else result[invalid]=invalid === "instance_id" ? "old" : "other";
  h.resolve(result); assert.equal(await pending,false);
  assert.equal(h.current.instance,"old"); assert.equal(h.current.session,"source"); assert.equal(h.prompt.value,"Source draft");
  assert.equal(h.current.unknown,true); assert.equal(h.current.connected,false); assert.equal(h.posts.length,1);
});

test("retired switch response cannot overwrite a replacement owner or draft", async () => {
  const h=actionHarness(), pending=h.act(); h.context.live={project:"other"};
  h.resolve(snapshot()); assert.equal(await pending,false);
  assert.equal(h.current.instance,"old"); assert.equal(h.prompt.value,"Source draft"); assert.equal(h.posts.length,1);
});

function flowHarness() {
  const nodes=new Map(), events=new Map();
  const region={dataset:{workspaceOpening:"true"},hidden:true}, notice={remove(){nodes.delete("#workspace-opening");}};
  nodes.set("#live-session",region);nodes.set("#workspace-opening",notice);
  const document={documentElement:{dataset:{}},body:{addEventListener(){}},querySelector:selector=>nodes.get(selector),querySelectorAll:()=>[],addEventListener:(name,fn)=>events.set(name,fn)};
  // Opening is a React projection; the controller still owns which mounted
  // region may be uncovered, including a superseding navigation.
  const window={SnowNavigation:{},SnowWorkspace:{
    updateDraft(){},
    updateOpening({visible}){if(!visible)nodes.delete("#workspace-opening");}
  },SnowShell:{navigation(open){events.get("snow:shell-command")?.({detail:{type:"navigation",open}});}}};
  const context=vm.createContext({document,window,localStorage:{getItem(){}},matchMedia:()=>({}),Element:class{},URL,TextEncoder,setNav(){},liveError(){},projectLocation:project=>`/?view=projects&project=${encodeURIComponent(project)}`});
  vm.runInContext(app.slice(0,app.indexOf("  function setTheme("))+`
    window.flow={consumeWorkspaceIntent,finishWorkspaceOpening,
      set:(owner,intent)=>{live=owner;workspaceIntent=intent;},
      draft:value=>{homeDraft=value;},getDraft:()=>homeDraft,
      visit:(project,session)=>visitedSessions.set(project,session),visited:project=>visitedSessions.get(project)};
  })();`,context);
  return {nodes,region,events,window:context.window,...context.window.flow};
}
test("cross-workspace cover stays until selection settles and cannot uncover a later navigation", async () => {
  const h=flowHarness();let done;
  h.window.SnowConversation={select:()=>new Promise(resolve=>{done=resolve;})};
  h.set({project:"p",session:"source",instance:"i",connected:true,status:"idle"},{project:"p",session:"target",instance:"i"});
  const pending=h.consumeWorkspaceIntent(); assert.equal(h.region.hidden,true);
  const newer={dataset:{workspaceOpening:"true"},hidden:true}, newerNotice={};
  h.nodes.set("#live-session",newer);h.nodes.set("#workspace-opening",newerNotice);
  done(true);await pending;assert.equal(newer.hidden,true);
  assert.equal(h.nodes.get("#workspace-opening"),newerNotice,"retired selection cannot clear the newer React opening cover");
});
for(const status of ["running","permission","input"])test(`${status} selection exposes original owner before Stop confirmation`,async()=>{
  const h=flowHarness();h.window.SnowConversation={select:async()=>{assert.equal(h.region.hidden,false);return true;}};
  h.set({project:"p",session:"source",instance:"i",connected:true,status},{project:"p",session:"target",instance:"i"});
  await h.consumeWorkspaceIntent();assert.equal(h.nodes.has("#workspace-opening"),false);
});
test("rejected switch exposes genuine warnings and is consumed once",async()=>{
  const h=flowHarness();let calls=0;h.window.SnowConversation={select:async()=>{calls++;return false;}};
  h.set({project:"p",session:"source",instance:"i",connected:true,status:"idle"},{project:"p",session:"target",instance:"i"});
  await h.consumeWorkspaceIntent();await h.consumeWorkspaceIntent();assert.equal(calls,1);assert.equal(h.region.hidden,false);
});
test("deleted cold target retires only its visit hint and retains draft as explicit new-session text",()=>{
  const h=flowHarness();h.visit("p","gone");h.visit("other","kept");h.draft({text:"Unsent",project:"p",targetSession:"gone",pending:true,ack:{instance:"old"}});
  // No visible draft host: the deletion receipt performs no navigation or action.
  h.events.get("snow:session-deleted")({detail:{project:"p",session:"gone"}});
  assert.equal(h.visited("p"),undefined);assert.equal(h.visited("other"),"kept");
  assert.equal(h.getDraft().text,"Unsent");assert.equal(h.getDraft().targetSession,"");assert.equal(h.getDraft().ack,undefined);
});
