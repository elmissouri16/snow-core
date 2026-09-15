import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const source = readFileSync(new URL('../../internal/web/static/versions.js', import.meta.url), 'utf8');
class Element {
  constructor() { this.children=[]; this.nodes=new Map(); this.dataset={}; this.attrs=new Map(); this.textContent=''; this.open=true; this.isConnected=false; this.replacements=0; }
  querySelector(selector) { if (!this.nodes.has(selector)) this.nodes.set(selector, new Element()); return this.nodes.get(selector); }
  querySelectorAll() { return []; }
  setAttribute(name,value) { this.attrs.set(name,value); }
  hasAttribute(name) { return this.attrs.has(name) || name==='data-version-preview' && Object.hasOwn(this.dataset,'versionPreview'); }
  append(...items) { for (const item of items) this.insertBefore(item,null); }
  insertBefore(item,at) { item.remove(); const i=at?this.children.indexOf(at):this.children.length;this.children.splice(i,0,item);item.parent=this; }
  remove() { if(this.parent){this.parent.children.splice(this.parent.children.indexOf(this),1);this.parent=null;} }
  replaceChildren(...items) { for(const item of [...this.children])item.remove();this.append(...items);this.replacements++; }
  get firstChild() { return this.children[0] || null; }
  get nextSibling() { return this.parent?.children[this.parent.children.indexOf(this)+1] || null; }
}
function fixture() {
  const root=new Element(), dialog=new Element(), document={createElement:()=>new Element(), createDocumentFragment:()=>new Element()};
  const context=vm.createContext({window:{},document,AbortController,TextEncoder,Date,console});
  vm.runInContext(source.replace('  window.SnowVersions =', '  globalThis.hooks={set:v=>view=v,list,preview,drawPreview,canRestore,selection};\n  window.SnowVersions ='),context);
  const identity={project_id:'project',instance_id:'instance',session_id:'session'};
  const selected={branch_id:'previous',tip_id:'previous-tip',name:'Previous',current:false};
  const page={...identity,revision:1,current_branch_id:'current',current_tip_id:'current-tip',versions:[selected],has_more:false,next_cursor:''};
  const preview={...identity,revision:1,branch_id:selected.branch_id,tip_id:selected.tip_id,messages:[{role:'assistant',text:'Previous page two'}],has_more:false,next_cursor:''};
  const view={api:{root,identity,list:async()=>structuredClone(page),preview:async()=>structuredClone(preview),restoreState(){}},dialog,ui:{readable:true,restoreSafe:true},snapshot:{revision:1},page,selected,preview,listCursors:['','next'],previewCursors:['','page-two'],listIndex:0,previewIndex:1};
  context.hooks.set(view);context.hooks.drawPreview();
  return {view,page,preview,selected,hooks:context.hooks,at:selector=>dialog.querySelector(selector)};
}

test('version list pagination cannot clear a failed preview/restore authority latch',async()=>{
  const f=fixture();f.view.stale=true;
  await f.hooks.list(1);
  assert.equal(f.view.stale,true);assert.equal(f.hooks.canRestore(),false);assert.equal(f.hooks.selection(),null);
  assert.equal(f.view.preview,f.preview);assert.equal(f.view.selected,f.selected);
});

test('pending/failed/identical refresh retains display only, with a truthful retained page number',async()=>{
  const f=fixture(), messages=f.at('[data-version-preview-messages]'), replacements=messages.replacements;
  let fail;
  f.view.api.list=()=>new Promise((_,reject)=>{fail=reject;});
  const pending=f.hooks.list(0,true);
  assert.equal(messages.replacements,replacements);assert.equal(f.view.selected,null);assert.equal(f.view.preview,null);
  assert.equal(f.hooks.canRestore(),false);assert.equal(f.hooks.selection(),null);
  fail(Error('offline'));await pending;
  assert.equal(messages.replacements,replacements);assert.equal(f.view.stale,true);assert.equal(f.hooks.canRestore(),false);
  f.view.api.list=async()=>structuredClone(f.page);await f.hooks.list(0,true);
  assert.equal(messages.replacements,replacements);assert.equal(f.view.stale,false);assert.equal(f.hooks.canRestore(),false);assert.equal(f.hooks.selection(),null);
  // A second refresh must not erase the still presentation-only cached preview.
  await f.hooks.list(0,true);assert.equal(messages.replacements,replacements);
  let reply;f.view.api.preview=()=>new Promise(resolve=>{reply=resolve;});
  const read=f.hooks.preview(f.view.page.versions[0]);
  assert.match(f.at('[data-version-preview-title]').textContent,/page 2/);
  assert.equal(messages.replacements,replacements);assert.equal(f.hooks.canRestore(),false);
  reply({...f.preview,messages:[{role:'assistant',text:'Verified page one'}]});await read;
  assert.match(f.at('[data-version-preview-title]').textContent,/page 1/);
  assert.equal(messages.replacements,replacements+1);assert.equal(f.view.retained,null);assert.equal(f.hooks.canRestore(),true);
});
