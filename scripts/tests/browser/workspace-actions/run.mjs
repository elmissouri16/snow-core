// Native production React shell routing; browser.mjs owns exported-page geometry.
import {component} from './component.mjs';
await component(async ({evaluate,check,click,key,settle,ids})=>{
  const selector=(i,control)=>`[data-sidebar-project="${ids[i]}"] [data-shell-project-${control}]`;
  await evaluate(`reply(0,[{session_id:'a1',name:'First'}]);`);await settle();
  await click(selector(0,'new'));
  await check('intents.length===1 && intents[0].type==="new" && intents[0].project===ids[0] && intents[0].trigger===newLink(0) && navigation.length===0','Cold New emits exactly one scoped owner intent, not incidental GET');
  await evaluate(`window.live={project:ids[0],session:'a1',instance:'worker-a',title:'First',renameAvailable:true,renameDisabled:false,newDisabled:false};SnowShell.updateLive(live);`);await settle();
  await click(selector(0,'new'));
  await check('intents.length===2 && intents[1].project===ids[0] && intents[1].trigger===newLink(0) && navigation.length===0','Live New delegates exactly one owner intent without hidden workflow');
  await click(selector(1,'new'));
  await check('intents.length===3 && intents[2].project===ids[1] && navigation.length===0','Other-workspace New emits one explicit scoped intent');
  await evaluate('SnowShell.updateLive({...live,newDisabled:true})');await settle();
  await click(selector(0,'new'));
  await check('newLink(0).getAttribute("aria-disabled")==="true" && intents.length===3 && navigation.length===0','Authoritatively disabled New cannot act');
  await click('[data-shell-new-session]');
  await check('intents.length===3 && navigation.length===0','Disabled global New cannot act');
  await evaluate('SnowShell.updateLive(live)');await settle();
  await click('[data-shell-new-session]');
  await check('intents.length===4 && intents[3].project===ids[0]','Global New uses current workspace exactly once');
  // Synthetic modifier dispatch is only for default-prevention inspection: native
  // unmodified action paths above and below use CDP input, never callback calls.
  await check(`[{ctrlKey:true},{metaKey:true},{shiftKey:true},{altKey:true},{button:1}].every(modifier=>{const event=new MouseEvent('click',{bubbles:true,cancelable:true,...modifier}); const prevent=e=>e.preventDefault();window.addEventListener('click',prevent,{once:true});const seen=[];const observe=e=>seen.push(e.defaultPrevented);document.addEventListener('click',observe,{once:true});newLink(0).dispatchEvent(event);return seen[0]===false;}) && intents.length===4 && navigation.length===0`,'Modified New preserves browser default without owner intent');
  await evaluate('SnowShell.updateLive(null)');await settle();
  await click('[data-shell-session="a1"] [data-shell-session-open]');
  // Live ownership remains stamped until validated inventory replaces it.
  await check('intents.length===5 && intents[4].type==="select" && intents[4].instance==="worker-a" && intents[4].session==="a1"','Live inventory link delegates exact session/instance selection');
  await evaluate(`SnowSidebarSessions.invalidate(ids[0]);reply(pending.length-1,[{session_id:'a1',name:'First'}]);`);await settle();
  await click('[data-shell-session="a1"] [data-shell-session-open]');
  await check('intents.length===5 && navigation.length===1 && new URL(navigation[0].href,location.href).searchParams.get("session")==="a1"','Cold inventory link navigates saved session without owner selection');
  await check('navigation[0].source===row("a1").querySelector("a") && navigation[0].native===true','Navigation callback carries the actual source and native navigation marker');
  await click(selector(0,'menu'));
  await check('$(".snow-menu").parentElement===document.body && more(0).getAttribute("aria-expanded")==="true"','React project popup uses real shared body portal');
  await check('JSON.stringify([...$(".snow-menu").querySelectorAll("[role=menuitem]")].map(x=>x.textContent))===JSON.stringify(["Workspace settings…","Remove registration…"])','Workspace menu offers only supported actions');
  await check('[...$(".snow-menu").querySelectorAll("a")].every(x=>x.hasAttribute("data-snow-navigation") && x.getAttribute("href").includes("session=a1"))','Only current workspace popup retains bootstrap saved session through native navigation markers');
  await key('Escape');
  await check('!$(".snow-menu") && document.activeElement===more(0)','Escape cleans up and restores launcher focus');
  await evaluate(`const inspector=document.createElement('section');inspector.id='project-inspector';inspector.dataset.project=ids[0];document.body.append(inspector);`);
  await click(selector(0,'menu'));await key('Home');await key('Enter');
  await check('inspections.length===1 && inspections[0].project===ids[0] && inspections[0].remove===false && inspections[0].trigger===more(0) && navigation.length===1','Current Settings emits presentation request, no navigation');
  await click(selector(0,'menu'));await key('End');await key('Enter');
  await check('inspections.length===2 && inspections[1].remove===true && inspections[1].trigger===more(0) && navigation.length===1 && !$(".snow-menu")','Current Remove only emits unchecked presentation request, no submit');
  await click(selector(1,'menu'));
  await check('[...$(".snow-menu").querySelectorAll("a")].every((x,i)=>x.getAttribute("href")==="/?view=projects&project="+ids[1]+"&inspect=project"+(i?"#remove-project":""))','Other workspace settings/removal URLs are canonical and session-free');
  await key('End');await key('Enter');
  await check('navigation.length===2 && navigation[1].href.endsWith("&inspect=project#remove-project") && navigation[1].native===true && inspections.length===2','Other workspace Remove delegates one navigation with exact fragment metadata');
  await click(selector(0,'menu'));
  await evaluate(`window.retiredNew=newLink(0);window.retiredItem=$('.snow-menu [role=menuitem]');swap();`);await settle();
  // Detached anchors still have browser defaults; isolate those from retired React intents.
  await evaluate('for(const node of [retiredNew,retiredItem]){node.addEventListener("click",e=>e.preventDefault(),{once:true});node.click();}');await settle();
  await check('!retiredNew.isConnected && !retiredItem.isConnected && intents.length===5 && inspections.length===2 && navigation.length===2 && !$(".snow-menu")','Unmounted React launchers and retired popup cannot act on replacement workspace');
  await evaluate('bootstrap.project="";bootstrap.session="";bootstrap.sessions=[];swap()');await settle();
  await click('[data-shell-new-session]');
  await check('navigation.length===3 && navigation[2].href==="/" && intents.length===5','Without a selected workspace global New retains home fallback');
  await check('calls.every(x=>x.method==="GET") && inspections.length===2 && intents.length===5 && navigation.length===3','Exact action totals; no hidden mutations or automatic replay after remount');
});
