// Shared native component fixture: production generated module + public facades.
// No React/DOM shim, source transform, dependency install, manager or worker.
import {spawn} from 'node:child_process';
import {createHash} from 'node:crypto';
import {readFile, mkdtemp, rm} from 'node:fs/promises';
import {createServer} from 'node:http';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {setTimeout as delay} from 'node:timers/promises';
import {chromeBinary, connect, debuggingURL} from '../live-stream/cdp.mjs';

export const ids = ['00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000003'];
export async function component(run) {
  const assets = new Map();
  for (const path of ['generated/app.js', 'menus.js']) assets.set('/static/' + path, await readFile(new URL('../../../../internal/web/static/' + path, import.meta.url)));
  const bundle = assets.get('/static/generated/app.js');
  console.log(`Production React module: ${bundle.length} bytes; sha256 ${createHash('sha256').update(bundle).digest('hex')}`);
  const bootstrap = {csrf: 'fixture-public-csrf', version: 'fixture', view: 'projects', project: ids[0], session: 'a1',
    hostSettingsEnabled: false, apiKeyEnabled: false, tls: false, pairingCode: '', live: null,
    projects: ids.map((id, i) => ({id, name: ['Alpha', 'Beta', 'Gamma'][i], path: '/fixture/' + i, available: true, trustRemembered: false, skillsEnabled: false, pinned: false})),
    sessions: [{session_id: 'a1', name: 'First'}]};
  const setup = `
    window.bootstrap = ${JSON.stringify(bootstrap)}; window.ids = ${JSON.stringify(ids)};
    window.calls=[]; window.browserReads=0; window.pending=[]; window.intents=[]; window.navigation=[]; window.inspections=[];
    window.fetch=(url,options={})=>{ if(calls.length + browserReads >= 256) return Promise.reject(Error('Fixture transport limit')); if(url==='/access/browsers' && (!options.method || options.method==='GET') && options.credentials==='same-origin' && options.cache==='no-store' && options.redirect==='error' && options.headers?.Accept==='application/json' && options.body===undefined){ browserReads++; return Promise.resolve(new Response(JSON.stringify({browsers:[],limit:8}),{headers:{'Content-Type':'application/json'}})); } calls.push({url, method:options.method || 'GET', credentials:options.credentials, accept:options.headers?.Accept});
      if (!/^\\/projects\\/[^/]+\\/sidebar-sessions\\?offset=\\d+$/.test(url) || (options.method && options.method !== 'GET')) return Promise.reject(Error('Unexpected fixture request'));
      return new Promise(resolve=>pending.push({url,options,resolve})); };
    window.reply=(index, rows, instance='', extra={})=>{const request=pending[index]; if(!request) throw Error('Missing deferred request '+index);
      request.resolve(new Response(JSON.stringify({project_id:request.url.split('/')[2],instance_id:instance,sessions:rows,available:true,has_more:false,...extra}),{headers:{'Content-Type':'application/json'}}));};
    document.addEventListener('snow:session-new',e=>intents.push({type:'new',...e.detail}));
    document.addEventListener('snow:session-select',e=>intents.push({type:'select',...e.detail}));
    document.addEventListener('snow:shell-navigate',e=>navigation.push({href:e.detail.href,source:e.detail.source,push:e.detail.source?.getAttribute('hx-push-url'),sync:e.detail.source?.getAttribute('hx-sync')}));
    document.addEventListener('snow:inspect-project',e=>inspections.push(e.detail));
    window.mountHost=()=>{ const host=document.createElement('main');host.id='workspace';host.dataset.project=bootstrap.project;host.dataset.session=bootstrap.session;
      host.innerHTML='<div id="shell-navigation-root"></div><div id="shell-react-root" data-react-page="shell"></div>';
      host.querySelector('[data-react-page]').dataset.reactProps=JSON.stringify(bootstrap);document.body.append(host); };
    window.swap=()=>{const old=document.querySelector('#workspace');document.dispatchEvent(new CustomEvent('htmx:beforeCleanupElement',{detail:{elt:old}}));old.remove();mountHost();document.dispatchEvent(new Event('htmx:afterSwap'));};
    window.$=s=>document.querySelector(s);window.group=i=>$('[data-sidebar-project="'+ids[i]+'"]');window.row=s=>$('[data-shell-session="'+s+'"]');
    window.newLink=i=>group(i).querySelector('[data-shell-project-new]');window.more=i=>group(i).querySelector('[data-shell-project-menu]');
    window.turns=()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));
    mountHost();`;
  const html = `<!doctype html><html><head><meta charset="utf-8"><style>body{margin:16px} .shell-project-row{display:flex;gap:12px}.shell-project-actions{display:flex}button,a{margin:3px}.snow-menu{background:white;min-width:220px}.snow-menu-row{display:block}.snow-menu-content{max-height:300px;overflow:auto}[hidden]{display:none!important}svg{width:16px;height:16px}</style></head><body><script>${setup}</script><script src="/static/menus.js"></script><script type="module" src="/static/generated/app.js"></script></body></html>`;
  const requests=[], diagnostics=[], external=[];
  const server=createServer((req,res)=>{if(requests.length>=256){res.writeHead(429);res.end();return;}requests.push({url:req.url,method:req.method});const body=req.url==='/'?html:assets.get(req.url);res.writeHead(body && req.method==='GET'?200:404,{'Content-Type':req.url==='/'?'text/html':'text/javascript'});res.end(req.url==='/favicon.ico'?'':body || 'Not found');});
  const temporary=await mkdtemp(join(tmpdir(),'snow-workspace-component-'));
  let chrome,client,timer,count=0;
  try {
    await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));const origin=`http://127.0.0.1:${server.address().port}`;
    chrome=spawn(chromeBinary(),['--headless','--disable-gpu','--no-first-run','--no-default-browser-check','--disable-background-networking','--disable-component-update','--disable-sync','--disable-extensions','--remote-debugging-port=0',`--user-data-dir=${temporary}`,'about:blank'],{stdio:['ignore','ignore','pipe']});
    await Promise.race([new Promise((_,reject)=>{timer=setTimeout(()=>reject(Error('Component checks exceeded 60s')),60000);}), (async()=>{
      client=await connect(await debuggingURL(chrome));const {targetId}=await client.send('Target.createTarget',{url:'about:blank'});
      const {sessionId}=await client.send('Target.attachToTarget',{targetId,flatten:true});const send=(method,params)=>client.send(method,params,sessionId);
      client.onEvent(event=>{if(event.sessionId!==sessionId)return;
        if(event.method==='Runtime.exceptionThrown' && diagnostics.length<20)diagnostics.push(event.params.exceptionDetails.text);
        if(event.method==='Runtime.consoleAPICalled' && ['error','warning'].includes(event.params.type) && diagnostics.length<20)diagnostics.push(event.params.args.map(x=>x.value || x.description).join(' '));
        if(event.method==='Network.requestWillBeSent' && !event.params.request.url.startsWith(origin+'/') && external.length<20)external.push(event.params.request.url);
      });
      await send('Page.enable');await send('Runtime.enable');await send('Network.enable');
      await send('Emulation.setDeviceMetricsOverride',{width:1280,height:900,deviceScaleFactor:1,mobile:false});
      const evaluate=async expression=>{const result=await send('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});if(result.exceptionDetails)throw Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);return result.result.value;};
      const wait=async(expression,label=expression)=>{for(let i=0;i<160;i++){if(await evaluate(expression))return;await delay(20);}throw Error('Timed out: '+label+'; '+JSON.stringify(await evaluate('({ready:window.SnowReactReady,mounted:document.querySelector("[data-react-page=shell]")?.dataset.reactMounted,calls:window.calls,text:document.body.textContent.slice(-700)})'))+'; '+JSON.stringify(diagnostics));};
      const check=async(expression,label)=>{if(await evaluate(expression)!==true)throw Error(label);count++;console.log('PASS',label);};
      const settle=()=>evaluate('turns()');
      const click=async selector=>{const point=await evaluate(`(()=>{const node=document.querySelector(${JSON.stringify(selector)});if(!node)throw Error('Missing native click target');node.scrollIntoView({block:'center'});const r=node.getBoundingClientRect(),x=r.x+r.width/2,y=r.y+r.height/2,hit=document.elementFromPoint(x,y);if(!r.width||!r.height||!(hit===node||node.contains(hit)))throw Error('Native click target obscured');return {x,y};})()`);await send('Input.dispatchMouseEvent',{type:'mousePressed',button:'left',clickCount:1,...point});await send('Input.dispatchMouseEvent',{type:'mouseReleased',button:'left',clickCount:1,...point});await settle();};
      const key=async(key)=>{const code={Enter:13,Escape:27,End:35,Home:36,ArrowDown:40}[key];await send('Input.dispatchKeyEvent',{type:'keyDown',key,code:key,windowsVirtualKeyCode:code,...(key==='Enter'?{text:'\r',unmodifiedText:'\r'}:{})});await send('Input.dispatchKeyEvent',{type:'keyUp',key,code:key,windowsVirtualKeyCode:code});await settle();};
      await send('Page.navigate',{url:origin+'/'});await wait('window.SnowReactReady && $("[data-react-page=shell]")?.dataset.reactMounted === "true" && calls.length===1');await settle();
      await run({evaluate,wait,check,click,key,settle,ids});
      await check('calls.every(x=>x.method==="GET" && x.credentials==="same-origin" && x.accept==="application/json")','All component inventory requests remain same-origin JSON GETs');
      if(diagnostics.length)throw Error('Browser/React diagnostics: '+JSON.stringify(diagnostics));count++;
      if(external.length)throw Error('External browser request');count++;
      if(requests.some(x=>x.method!=='GET' || !(x.url==='/' || x.url==='/favicon.ico' || assets.has(x.url))))throw Error('Unexpected server request');count++;
      console.log(`${count} native production React assertions passed`);
    })()]);
  } finally {
    clearTimeout(timer);client?.close();if(chrome && chrome.exitCode===null){const stopped=new Promise(resolve=>chrome.once('exit',resolve));chrome.kill('SIGKILL');await stopped;}
    await new Promise(resolve=>server.close(resolve));await rm(temporary,{recursive:true,force:true,maxRetries:5,retryDelay:100});
  }
}
