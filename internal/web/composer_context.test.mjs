import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
const source = fs.readFileSync(new URL('./static/composer-context.js', import.meta.url), 'utf8');
class Element {
  constructor(tag = 'div') {
    this.tagName = tag; this.children = []; this.attrs = {}; this.events = {}; this.value = ''; this.selectionStart = this.selectionEnd = 0;
    this.hidden = false; this.disabled = false; this.className = ''; this.textContent = ''; this.scrollTop = 0; this.clientHeight = 200;
    this.classList = {toggle: () => {}};
  }
  append(...nodes) { this.children.push(...nodes); }
  replaceChildren(...nodes) { this.children = nodes; }
  setAttribute(name, value) { this.attrs[name] = String(value); }
  removeAttribute(name) { delete this.attrs[name]; }
  getAttribute(name) { return this.attrs[name]; }
  addEventListener(name, handler, options = {}) {
    (this.events[name] ||= []).push({handler, signal: options.signal});
  }
  dispatchEvent(event) { for (const {handler, signal} of this.events[event.type] || []) if (!signal?.aborted) handler(event); }
  dispatch(type, fields = {}) {
    const event = {type, preventDefault() { this.defaultPrevented = true; }, stopPropagation() {}, ...fields};
    this.dispatchEvent(event); return event;
  }
  querySelectorAll(selector) {
    const matches = child => selector.startsWith('.') ? child.className.split(' ').includes(selector.slice(1)) : selector === '[role="option"]' && child.attrs.role === 'option';
    return this.children.flatMap(child => [...(matches(child) ? [child] : []), ...child.querySelectorAll(selector)]);
  }
  querySelector(selector) { return this.querySelectorAll(selector)[0] || null; }
  closest() { return this.inert ? this : null; }
  focus() { this.focused = true; }
  click() { if (!this.disabled) this.dispatch('click'); }
  setRangeText(text, start, end) { this.value = this.value.slice(0, start) + text + this.value.slice(end); this.selectionStart = this.selectionEnd = start + text.length; }
}
function environment(windowOptions = {}, previewAPI = URL) {
  const window = {...windowOptions}, document = {
    createElement: tag => new Element(tag),
    createElementNS: (namespace, tag) => { const element = new Element(tag); element.namespaceURI = namespace; return element; },
  };
  vm.runInNewContext(source, {window, document, TextEncoder, TextDecoder, AbortController, Uint8Array, Event, btoa, atob, Blob, URL: previewAPI, console});
  return window.SnowComposerContext;
}
function fixture(api = environment(), key = 'project:session', instance = 'instance', compact = false) {
  const nodes = {}, selectors = ['#live-prompt', '#live-composer', '[data-composer-file-input]', '[data-composer-attach]', '[data-composer-files]', '[data-composer-skills]', '[data-composer-context-items]', '[data-composer-context-status]', '[data-composer-mentions]'];
  for (const selector of selectors) nodes[selector] = new Element();
  if (compact) {
    delete nodes['[data-composer-files]']; delete nodes['[data-composer-skills]'];
    nodes['[data-composer-context-menu]'] = new Element('button');
  }
  const root = {querySelector: selector => nodes[selector]}, calls = [], pending = [], errors = []; let changes = 0;
  const controller = api.init({root,key,instance,changed: () => changes++,error: message => errors.push(message), request: (kind, fields) => {
    calls.push({kind,fields}); return new Promise((resolve, reject) => pending.push({resolve,reject}));
  }});
  controller.render({safe: true, editable: true, readable: true});
  const prompt = nodes['#live-prompt'], popup = nodes['[data-composer-mentions]'];
  const type = text => { prompt.value = text; prompt.selectionStart = prompt.selectionEnd = text.length; prompt.dispatch('input'); };
  const add = (...files) => { nodes['[data-composer-file-input]'].files = files; nodes['[data-composer-file-input]'].dispatch('change'); };
  const options = () => popup.querySelectorAll('[role="option"]');
  return {api,controller,nodes,prompt,popup,type,add,options,calls,pending,errors, changes: () => changes, reply: value => pending.shift().resolve(value)};
}
const tick = async () => { for (let i = 0; i < 10; i++) await Promise.resolve(); };
const file = (name, text, type = 'application/octet-stream') => {
  const data = typeof text === 'string' ? new TextEncoder().encode(text) : new Uint8Array(text);
  return {name,type,size:data.length,arrayBuffer: async () => data.buffer};
};
const listing = (entries = [], extra = {}) => ({path:'.',entries,next_offset:entries.length,has_more:false,...extra});
const entry = (name, kind = 'file') => ({name,path:name,kind});
const catalog = (extra = {}) => ({project_id:'project',session_id:'session',instance_id:'instance',enabled:true,skills:[{name:'find-docs',description:'Find references',enabled:true}],...extra});

test('initialization and render do not discover or upload; typed ordinary tokens alone are literal', async () => {
  const f = fixture(); assert.equal(f.calls.length, 0); assert.equal(f.controller.capture('hello').hasContent, false);
  f.type('Review @README.md please'); await tick(); assert.equal(f.calls.length,0);
  assert.equal(f.controller.capture().text,'Review @README.md please');
});
test('local UTF-8 text ignores browser MIME and accepted removes only exact captured attachments', async () => {
  const f = fixture(); f.add(file('<img onerror=bad>.go', 'package demo')); assert.equal(f.controller.pending(), true);
  assert.throws(() => f.controller.capture('hello'), /reads/); await tick();
  const capture = f.controller.capture('Review'); const content = JSON.parse(capture.content);
  assert.equal(capture.text, 'Review');
  assert.deepEqual(content, [{type:'text',text:'Attachment: <img onerror=bad>.go\npackage demo'}]);
  assert.equal(f.nodes['[data-composer-context-items]'].children[0].children[0].textContent, '<img onerror=bad>.go');
  f.add(file('later.txt','keep this')); await tick();
  f.controller.accepted(capture); assert.equal(f.controller.hasAttachments(),true);
  const remainingCapture = f.controller.capture('');
  const remaining = JSON.parse(remainingCapture.content); assert.equal(remainingCapture.text,'Please review the attached files.'); assert.match(remaining[0].text,/later.txt/);
  f.controller.accepted(capture); assert.equal(f.controller.capture('').items.length,1);
});
test('failed sends do not consume attachments and restored drafts retain ready items', async () => {
  const f = fixture(); f.add(file('a.txt','hello')); await tick(); const captured = f.controller.capture('send');
  f.controller.dispose(); const next = fixture(f.api); assert.equal(next.controller.hasAttachments(),true);
  next.controller.accepted(captured); assert.equal(next.controller.hasAttachments(),true);
  next.controller.accepted(next.controller.capture('retry')); assert.equal(next.controller.hasAttachments(),false);
});
test('invalid Unicode, binary, PDFs and fake image signatures leave visible removable errors', async () => {
  for (const sample of [file('nul.txt','a\0b'),file('bad.txt',[255,254]),file('paper.pdf','%PDF-1.7'),file('fake.png','plain text','image/png')]) {
    const f = fixture(); f.add(sample); await tick(); assert.equal(f.controller.pending(),false);
    assert.throws(() => f.controller.capture('send'),/failed attachments/);
    const chip = f.nodes['[data-composer-context-items]'].children[0]; assert.ok(chip.children[1].textContent.length);
    chip.children.at(-1).click(); assert.equal(f.controller.hasAttachments(),false);
  }
  assert.throws(() => fixture().controller.capture('\ud800'),/Unicode/);
});
test('supported image signatures encode protocol blocks; total raw image bytes are bounded', async () => {
  for (const [signature,mime] of [[[137,80,78,71,13,10,26,10],'image/png'],[[255,216,255],'image/jpeg'],[new TextEncoder().encode('GIF89a'),'image/gif'],[new TextEncoder().encode('RIFF0000WEBP'),'image/webp']]) {
    const f = fixture(); f.add(file('image',signature)); await tick(); const blocks = JSON.parse(f.controller.capture('').content);
    assert.equal(blocks[1].type,'image'); assert.equal(blocks[1].mime_type,mime); assert.ok(blocks[1].data);
  }
  const f = fixture(), image = new Uint8Array(1024*1024+1); image.set([255,216,255]);
  f.add(file('first.jpg',image)); await tick(); f.add(file('second.jpg',image)); await tick();
  assert.throws(() => f.controller.capture('send'), /failed/);
});
test('text byte budgets count labels, multibyte content and combined prompt', async () => {
  const f = fixture(); f.add(file('a.txt','é'.repeat(32768))); await tick(); assert.throws(() => f.controller.capture(''),/failed/);
  const g = fixture(); g.add(file('b.txt','x'.repeat(63000))); await tick();
  assert.throws(() => g.controller.capture('a'.repeat(70000)),/128 KiB/);
  assert.equal(g.controller.capture('small').hasContent,true);
});
test('at most eight read slots and bounded pending size, with no unbounded FileList reads', async () => {
  const f = fixture(); let reads = 0; const slow = name => ({name,size:10,type:'',arrayBuffer:() => { reads++; return new Promise(() => {}); }});
  f.add(...Array.from({length:100},(_,i) => slow(String(i)))); assert.equal(reads,8); assert.ok(f.errors.length);
  const g = fixture(); g.add({...slow('oversized'),size:2*1024*1024+1}); assert.equal(g.controller.pending(),false); assert.equal(g.controller.hasAttachments(),false);
});
test('dispose fences pending local reads; restored pending entry is an explicit error, never stale content', async () => {
  const f = fixture(); let resolve;
  f.add({name:'slow.txt',size:3,type:'',arrayBuffer:() => new Promise(done => {resolve = done;})});
  f.controller.dispose(); const next = fixture(f.api); resolve(new TextEncoder().encode('old').buffer); await tick();
  assert.equal(next.controller.pending(),false); assert.throws(() => next.controller.capture(''),/failed/);
  assert.match(next.nodes['[data-composer-context-items]'].children[0].children[1].textContent,/interrupted/);
});
test('sixteen drafts maximum and forget explicitly drops the selected key', async () => {
  const api = environment(); const first = fixture(api,'p:first'); first.add(file('first.txt','a')); await tick(); first.controller.dispose();
  for (let i=0;i<16;i++) { const f = fixture(api,`p:${i}`); f.add(file('a.txt','a')); await tick(); f.controller.dispose(); }
  assert.equal(fixture(api,'p:first').controller.hasAttachments(),false);
  api.forget('p:15'); assert.equal(fixture(api,'p:15').controller.hasAttachments(),false);
});
test('files are bounded direct directory discovery; typing filters locally; only choice reads', async () => {
  const f = fixture(); f.type('@'); assert.equal(f.controller.pending(),true); assert.equal(f.calls[0].kind,'files'); assert.deepEqual({...f.calls[0].fields},{path:'.',offset:0});
  f.reply(listing([entry('a.txt'),entry('b.txt'),entry('src','directory')])); await tick();
  f.type('@a'); assert.equal(f.calls.length,1); assert.equal(f.options().length,1);
  assert.equal(f.controller.hasAttachments(),false); f.options()[0].click(); assert.equal(f.calls[1].kind,'file');
  f.reply({path:'a.txt',text:'full contents',size:13,truncated:false}); await tick();
  assert.equal(f.prompt.value,'@"a.txt" '); assert.equal(f.controller.hasAttachments(),true); assert.equal(f.controller.pending(),false);
});
test('folder choice inserts a quoted prefix then lists just that directory; More is explicit', async () => {
  const f = fixture(); f.type('@'); f.reply(listing([entry('my dir','directory')],{has_more:true,next_offset:256})); await tick();
  f.options().at(-1).click(); assert.equal(f.calls[1].fields.offset,256);
  f.reply(listing([entry('later.txt')],{next_offset:512})); await tick(); f.options()[0].click();
  assert.equal(f.prompt.value,'@"my dir/'); assert.equal(f.calls[2].fields.path,'my dir');
  f.reply(listing([],{path:'my dir'})); await tick();
});
test('truncated project previews are rejected, rather than silently attached', async () => {
  const f = fixture(); f.type('@'); f.reply(listing([entry('a.txt')])); await tick(); f.options()[0].click();
  f.reply({path:'a.txt',text:'partial',size:100000,truncated:true}); await tick();
  assert.equal(f.prompt.value,'@'); assert.throws(() => f.controller.capture('send'),/failed/);
});
test('caret changes, replacement queries, disconnected authority and disposed scopes fence async results', async () => {
  for (const invalidate of [f => {f.prompt.selectionStart=0;},f => f.type('another prompt'), f => f.controller.render({safe:false}), f => f.controller.dispose()]) {
    const f = fixture(); f.type('@'); invalidate(f); f.reply(listing([entry('late.txt')])); await tick(); assert.equal(f.options().length,0); assert.equal(f.controller.hasAttachments(),false);
  }
  const f = fixture(); f.type('@'); f.reply(listing([entry('a.txt')])); await tick(); f.options()[0].click(); f.type('changed');
  f.reply({path:'a.txt',text:'hello',size:5,truncated:false}); await tick(); assert.equal(f.prompt.value,'changed'); assert.throws(() => f.controller.capture(''),/failed/);
});
test('skill catalog is lazy and once per runtime, inserts exact token without reading skills', async () => {
  const f = fixture(); f.nodes['[data-composer-skills]'].click(); await tick(); assert.equal(f.calls[0].kind,'skills'); assert.equal(f.controller.pending(),true);
  f.reply(catalog()); await tick(); f.options()[0].click(); assert.equal(f.prompt.value,'$find-docs '); assert.equal(f.controller.hasAttachments(),false);
  f.type('$find'); await tick(); assert.equal(f.calls.length,1); assert.equal(f.options().length,1);
  f.controller.dispose(); const next=fixture(f.api); next.type('$'); await tick(); assert.equal(next.calls.length,0); assert.equal(next.options().length,1);
});
test('disabled or malformed skill catalogs never forge a selectable skill', async () => {
  const f=fixture(); f.type('$'); await tick(); f.reply(catalog({enabled:false})); await tick(); assert.match(f.popup.children[0].textContent,/Close and start with Enable installed skills/); assert.equal(f.options().length,0);
  const g=fixture(); g.type('$'); await tick(); g.reply(catalog({skills:[{name:'bad name',enabled:true},{name:'disabled',enabled:false},{name:'safe',enabled:true}]})); await tick();
  assert.equal(g.options().length,2); g.options()[0].click(); assert.equal(g.prompt.value,'$'); g.options()[1].click(); assert.equal(g.prompt.value,'$safe ');
  const h=fixture(); h.type('$'); await tick(); h.reply(catalog({instance_id:'other'})); await tick(); assert.equal(h.options().length,1); assert.equal(h.options()[0].querySelector('.composer-mention-name').textContent, 'Retry loading skills');
});
test('textarea owns keyboard focus and listbox selection; modifiers and IME never select', async () => {
  const f=fixture(); f.type('$'); await tick(); f.reply(catalog({skills:[{name:'one',enabled:true},{name:'two',enabled:true}]})); await tick();
  assert.equal(f.prompt.getAttribute('aria-expanded'),'true');
  for (const fields of [{ctrlKey:true},{metaKey:true},{isComposing:true}]) { const event=f.prompt.dispatch('keydown',{key:'Enter',...fields}); assert.equal(event.defaultPrevented,undefined); assert.equal(f.prompt.value,'$'); }
  f.prompt.dispatch('keydown',{key:'ArrowDown'}); assert.equal(f.options()[1].getAttribute('aria-selected'),'true');
  f.prompt.dispatch('keydown',{key:'Enter'}); assert.equal(f.prompt.value,'$two '); assert.equal(f.prompt.focused,true);
  f.type('$'); await tick(); f.prompt.dispatch('keydown',{key:'Escape'}); assert.equal(f.popup.hidden,true);
});
test('editing, unreadable runtime, hidden/inert normal composer refuse addition and discovery', async () => {
  const f=fixture(); f.controller.render({editable:false}); f.add(file('a.txt','a')); f.type('@'); assert.equal(f.controller.hasAttachments(),false); assert.equal(f.calls.length,0);
  f.controller.render({editable:true,readable:false}); f.type('$'); await tick(); assert.equal(f.calls.length,0);
  f.prompt.inert=true; f.controller.render({readable:true});
  f.nodes['#live-composer'].dispatch('drop',{dataTransfer:{files:[file('a.txt','a')]}});
  f.nodes['#live-composer'].dispatch('paste',{clipboardData:{items:[{kind:'file',type:'image/png',getAsFile:()=>file('a.png',[137,80,78,71,13,10,26,10],'image/png')}]}});
  await tick(); assert.equal(f.controller.hasAttachments(),false);
});

test('capture keeps the prompt separate from attachment blocks and filename skill tokens out of Message', async () => {
  const f = fixture(); f.add(file('a $find-docs b.txt', 'attachment text')); await tick();
  const captured = f.controller.capture('Original prompt $selected-skill');
  assert.equal(captured.text, 'Original prompt $selected-skill');
  const content = JSON.parse(captured.content);
  assert.equal(content.length, 1);
  assert.deepEqual(content, [{type: 'text', text: 'Attachment: a $find-docs b.txt\nattachment text'}]);
  assert.equal(captured.content.includes('Original prompt'), false);
  const attachmentOnly = f.controller.capture('');
  assert.equal(attachmentOnly.text, 'Please review the attached files.');
  assert.equal(attachmentOnly.text.includes('$find-docs'), false);
  assert.deepEqual(JSON.parse(fixture().controller.capture('text only').content), []);
});
test('eight image attachments produce exactly sixteen blocks, with vision and history notice', async () => {
  const f = fixture();
  f.add(...Array.from({length: 8}, (_, i) => file(`image-${i}.png`, [137,80,78,71,13,10,26,10], 'image/png')));
  await tick();
  const captured = f.controller.capture('Look at these');
  assert.equal(captured.items.length, 8);
  assert.equal(captured.text, 'Look at these');
  const blocks = JSON.parse(captured.content);
  assert.equal(blocks.length, 16);
  assert.equal(blocks.filter(block => block.type === 'image').length, 8);
  assert.equal(blocks.filter(block => block.type === 'text').length, 8);
  const notice = f.nodes['[data-composer-context-status]'].textContent;
  assert.match(notice, /On Send: shared with provider and saved in chat/);
  assert.match(f.nodes['[data-composer-context-status]'].title, /Text and image contents.*provider.*persisted in saved conversation history/);
  assert.match(notice, /Images need a vision-capable model/);
});
test('accepted success clears captured attachments even while editing is disabled, preserving later additions', async () => {
  const f = fixture(); f.add(file('sent.txt', 'sent')); await tick();
  const captured = f.controller.capture('Send');
  f.add(file('later.txt', 'kept')); await tick();
  f.controller.render({safe: false, editable: false, readable: false});
  f.controller.accepted(captured);
  const remaining = f.controller.capture('Next');
  assert.equal(remaining.items.length, 1);
  assert.match(remaining.content, /later.txt/);
  assert.equal(remaining.content.includes('sent.txt'), false);
});

test('local removal remains available with unsafe runtime while adding and discovery stay disabled', async () => {
  const f = fixture(); f.add(file('kept.txt', 'kept')); await tick();
  f.controller.render({safe: false, editable: true, readable: false});
  const remove = f.nodes['[data-composer-context-items]'].children[0].children.at(-1);
  assert.equal(remove.disabled, false);
  f.add(file('not-added.txt', 'blocked')); f.type('$'); await tick();
  assert.equal(f.controller.capture('').items.length, 1);
  assert.equal(f.calls.length, 0);
  assert.equal(f.controller.pending(), false);
  remove.click();
  assert.equal(f.controller.hasAttachments(), false);
  assert.equal(f.calls.length, 0);
});
test('local removal still respects editable and inert composer boundaries', async () => {
  for (const block of [f => f.controller.render({safe:false, editable:false}), f => {f.prompt.inert=true; f.controller.render({safe:false});}]) {
    const f = fixture(); f.add(file('kept.txt', 'kept')); await tick(); block(f);
    const remove = f.nodes['[data-composer-context-items]'].children[0].children.at(-1);
    assert.equal(remove.disabled, true); remove.click(); assert.equal(f.controller.hasAttachments(), true);
  }
});
test('failed skill discovery retries only on an explicit retry choice, not typing or reopening', async () => {
  const f = fixture(); f.type('$'); await tick();
  f.pending.shift().reject(new Error('temporary failure')); await tick();
  assert.equal(f.controller.pending(), false);
  assert.equal(f.options()[0].querySelector('.composer-mention-name').textContent, 'Retry loading skills');
  assert.equal(f.options()[0].getAttribute('data-composer-skills-retry'), '');
  f.type('$find'); await tick(); assert.equal(f.calls.length, 1);
  f.type('ordinary text'); f.type('$'); await tick(); assert.equal(f.calls.length, 1);
  const retry = f.options()[0]; retry.click(); retry.click(); await tick();
  assert.equal(f.calls.length, 2); assert.equal(f.controller.pending(), true);
  f.reply(catalog()); await tick();
  assert.equal(f.options()[0].querySelector('.composer-mention-name').textContent, '$find-docs');
  f.options()[0].click(); assert.equal(f.prompt.value, '$find-docs ');
});
test('stale retry controls cannot discover for a changed or disposed query', async () => {
  for (const invalidate of [f => f.type('changed'), f => f.controller.dispose(), f => f.controller.render({safe:false})]) {
    const f = fixture(); f.type('$'); await tick(); f.pending.shift().reject(new Error('offline')); await tick();
    const retry = f.options()[0]; invalidate(f); retry.click(); await tick(); assert.equal(f.calls.length, 1);
  }
});

test('selected project filenames cannot introduce whitespace-delimited skill activation tokens', async () => {
  const f = fixture(), name = 'a $foo b.go';
  f.type('@'); f.reply(listing([entry(name)])); await tick(); f.options()[0].click();
  f.reply({path:name, text:'package demo', size:12, truncated:false}); await tick();
  assert.equal(f.prompt.value, '@"a \\u0024foo b.go" ');
  assert.equal(/(?:^|\s)\$foo(?:\s|$)/.test(f.controller.capture().text), false);
  assert.equal(JSON.parse(f.prompt.value.slice(1).trim()), name);
  assert.match(f.controller.capture().content, /a \$foo b.go/);
});
test('selected folder names escape skill tokens but decode the actual directory for discovery', async () => {
  const f = fixture(), name = 'a $foo folder';
  f.type('@'); f.reply(listing([entry(name, 'directory')])); await tick(); f.options()[0].click();
  assert.equal(f.prompt.value, '@"a \\u0024foo folder/');
  assert.equal(/(?:^|\s)\$foo(?:\s|$)/.test(f.prompt.value), false);
  assert.equal(f.calls[1].fields.path, name);
  f.reply(listing([],{path:name})); await tick();
});
test('picker geometry stays viewport-bounded and usable when above-composer clearance is tiny', async () => {
  for (const bounds of [
    {height:240, formTop:40, inputTop:52, inputBottom:76},
    {height:240, formTop:90, inputTop:100, inputBottom:124},
    {height:800, formTop:620, inputTop:632, inputBottom:684},
  ]) {
    const f = fixture(environment({innerWidth:320, innerHeight:bounds.height}));
    f.nodes['#live-composer'].getBoundingClientRect = () => ({top:bounds.formTop,left:12,width:296});
    f.prompt.getBoundingClientRect = () => ({top:bounds.inputTop,bottom:bounds.inputBottom});
    f.popup.style = {};
    Object.defineProperty(f.popup, 'offsetHeight', {get: () => Math.min(400, Number.parseFloat(f.popup.style.maxHeight) || 400)});
    f.type('$'); await tick(); f.reply(catalog()); await tick();
    const height = f.popup.offsetHeight, top = Number.parseFloat(f.popup.style.top), left = Number.parseFloat(f.popup.style.left), width = Number.parseFloat(f.popup.style.width);
    assert.ok(height >= 120); assert.ok(top >= 8); assert.ok(top + height <= bounds.height - 8);
    assert.ok(left >= 8); assert.ok(left + width <= 312);
    if (bounds.formTop === 40) assert.ok(top >= bounds.inputBottom, 'short viewport keeps textarea visible when space exists below');
    if (bounds.height === 800) {
      assert.ok(top + height < bounds.formTop, 'ordinary viewport anchors above the composer');
      assert.equal(f.popup.style.maxHeight, '320px', 'desktop allows seven compact rows and a heading');
      assert.equal(height, 320);
    }
  }
});


function menusFixture() {
  let current = null;
  const closes = [];
  const menus = {
    open(menu) { if (current) menus.close({restoreFocus:false}); current = menu; },
    close(options = {}) { closes.push(options); const previous = current; current = null; previous?.onClose?.(); },
  };
  const api = environment({SnowMenus:menus}), f = fixture(api, 'project:session', 'instance', true);
  return {...f, menus, closes, menu: () => current, plus: f.nodes['[data-composer-context-menu]']};
}
test('compact plus opens a passive shared menu; only an explicit row inserts and discovers', async () => {
  for (const [index, hook, label, marker, kind] of [[0,'files','Project files','@','files'], [1,'skills','Installed skills','$','skills']]) {
    const f = menusFixture(); f.type('Explain this'); f.plus.click(); await tick();
    assert.equal(f.calls.length, 0); assert.equal(f.controller.pending(), false);
    assert.equal(f.prompt.value, 'Explain this'); assert.equal(f.popup.hidden, true);
    const menu = f.menu(); assert.equal(menu.trigger, f.plus); assert.equal(menu.placement, 'top-start');
    assert.equal(menu.panel.children.length, 2);
    const row = menu.panel.children[index];
    assert.ok(row.className.split(' ').includes('snow-menu-row'));
    assert.equal(row.getAttribute('role'), 'menuitem'); assert.equal(row.getAttribute(`data-composer-${hook}`), '');
    assert.equal(row.querySelector('.snow-menu-row-label').textContent, label);
    row.click(); await tick();
    assert.equal(f.menu(), null); assert.equal(f.closes.at(-1).restoreFocus, false);
    assert.equal(f.prompt.focused, true); assert.equal(f.prompt.value, `Explain this ${marker}`);
    assert.deepEqual(f.calls.map(call => call.kind), [kind]);
    row.click(); await tick(); assert.equal(f.calls.length, 1, 'closed rows are stale');
    f.controller.dispose();
  }
});
test('plus menu ownership closes on unsafe state or disposal and fences retired callbacks', async () => {
  for (const invalidate of [f => f.controller.render({safe:false}), f => f.controller.render({readable:false}), f => f.controller.dispose(), f => f.api.init({root:{querySelector:()=>null}})]) {
    const f = menusFixture(); f.plus.click(); const menu = f.menu(), row = menu.panel.children[0];
    // Even a failed replacement first disposes its predecessor.
    try { invalidate(f); } catch (reason) { assert.match(reason.message, /markup is incomplete/); }
    assert.equal(f.menu(), null); assert.equal(f.closes.at(-1).restoreFocus, false);
    row.click(); menu.onClose(); await tick(); assert.equal(f.calls.length, 0); assert.equal(f.prompt.value, '');
  }
  const f = menusFixture(); f.plus.click(); const stale = f.menu();
  f.menus.close(); f.plus.click(); const replacement = f.menu();
  stale.onClose(); stale.panel.children[1].click(); await tick();
  assert.equal(f.menu(), replacement); assert.equal(f.calls.length, 0);
  f.menus.open({panel:new Element(), trigger:new Element()}); const other = f.menu();
  f.controller.dispose(); assert.equal(f.menu(), other, 'disposal must not close another owner’s menu');
});
test('plus toggling and capped file choice remain passive while skills remain available', async () => {
  const f = menusFixture(); f.plus.click(); f.plus.click(); await tick();
  assert.equal(f.menu(), null); assert.equal(f.calls.length, 0);
  f.add(...Array.from({length:8}, (_, i) => file(`${i}.txt`, 'text'))); await tick();
  f.plus.click(); assert.equal(f.plus.disabled, false);
  assert.equal(f.menu().panel.children[0].disabled, true); assert.equal(f.menu().panel.children[1].disabled, false);
  f.menu().panel.children[0].click(); await tick(); assert.equal(f.calls.length, 0);
});
test('compact file rows retain exact titles with horizontal folder cues and selected-only hints', async () => {
  const f = fixture(), name = '<img onerror=bad>.go';
  f.type('@'); f.reply(listing([entry('folder', 'directory'), entry(name)])); await tick();
  assert.equal(f.popup.children[0].className, 'composer-mention-heading');
  assert.equal(f.popup.children[0].textContent, 'Files & folders');
  const [folder, regular] = f.options();
  assert.equal(folder.querySelector('.composer-mention-name').textContent, 'folder');
  assert.equal(folder.querySelector('.composer-mention-chevron').textContent, '›');
  assert.equal(folder.querySelector('.composer-mention-icon').getAttribute('data-kind'), 'folder');
  const folderSVG = folder.querySelector('.composer-mention-icon').children[0];
  assert.equal(folderSVG.tagName, 'svg'); assert.equal(folderSVG.namespaceURI, 'http://www.w3.org/2000/svg');
  assert.equal(folderSVG.getAttribute('viewBox'), '0 0 24 24');
  assert.equal(folderSVG.getAttribute('stroke'), 'currentColor'); assert.equal(folderSVG.getAttribute('stroke-width'), '1.6');
  assert.equal(folderSVG.getAttribute('fill'), 'none'); assert.equal(folderSVG.getAttribute('focusable'), 'false');
  assert.equal(folderSVG.children.length, 1);
  assert.equal(folderSVG.children[0].getAttribute('d'), 'M3 6a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z');
  assert.equal(folder.querySelector('.composer-mention-folder-hint').hidden, false);
  assert.equal(folder.querySelector('.composer-mention-description'), null);
  assert.equal(regular.querySelector('.composer-mention-description'), null);
  assert.equal(regular.querySelector('.composer-mention-name').textContent, name);
  assert.equal(regular.querySelector('.composer-mention-name').title, name); assert.equal(regular.title, name);
  assert.equal(regular.querySelector('.composer-mention-icon').getAttribute('aria-hidden'), 'true');
  assert.equal(regular.querySelector('.composer-mention-icon').getAttribute('data-kind'), 'file');
  const fileSVG = regular.querySelector('.composer-mention-icon').children[0];
  assert.equal(fileSVG.tagName, 'svg'); assert.equal(fileSVG.namespaceURI, 'http://www.w3.org/2000/svg');
  assert.equal(fileSVG.children.length, 2); assert.equal(fileSVG.children[0].tagName, 'rect');
  assert.deepEqual(fileSVG.children[0].attrs, {x:'5',y:'3',width:'14',height:'18',rx:'3'});
  assert.equal(fileSVG.children[1].getAttribute('d'), 'M8 8h8M8 12h6');
  assert.equal(regular.querySelector('.composer-mention-icon').textContent, '');
  f.prompt.dispatch('keydown', {key:'ArrowDown'});
  assert.equal(folder.querySelector('.composer-mention-folder-hint').hidden, true);
  f.prompt.dispatch('keydown', {key:'ArrowUp'});
  assert.equal(folder.querySelector('.composer-mention-folder-hint').hidden, false);
});
test('compact skill description is one label node with full exact metadata in titles', async () => {
  const f = fixture(), description = '<script>bad()</script>\n' + 'long metadata '.repeat(80);
  f.type('$'); await tick(); f.reply(catalog({skills:[{name:'find-docs', description, enabled:true}]})); await tick();
  assert.equal(f.popup.children[0].textContent, 'Skills');
  const row = f.options()[0], main = row.querySelector('.composer-mention-main'), detail = row.querySelector('.composer-mention-description');
  assert.equal(row.querySelector('.composer-mention-icon').textContent, '');
  assert.equal(row.querySelector('.composer-mention-icon').getAttribute('data-kind'), 'skill');
  assert.equal(row.querySelector('.composer-mention-icon').children[0].namespaceURI, 'http://www.w3.org/2000/svg');
  assert.equal(main.children.length, 2); assert.equal(detail.children.length, 0);
  assert.equal(detail.textContent, description.slice(0,512)); assert.equal(detail.title, description);
  assert.equal(row.title, `$find-docs — ${description}`);
});
test('mention width follows the composer rather than 420px and clamps to visual viewport', async () => {
  for (const bounds of [
    {width:1200, formWidth:780, formLeft:200, expectedWidth:780, expectedLeft:200},
    {width:1200, formWidth:180, formLeft:30, expectedWidth:180, expectedLeft:30},
    {width:320, formWidth:780, formLeft:-30, expectedWidth:304, expectedLeft:8},
    {width:1200, formWidth:780, formLeft:200, viewport:{width:500,height:400,offsetLeft:100,offsetTop:20,addEventListener() {}}, expectedWidth:484, expectedLeft:108},
  ]) {
    const f = fixture(environment({innerWidth:bounds.width,innerHeight:800,visualViewport:bounds.viewport}));
    f.nodes['#live-composer'].getBoundingClientRect = () => ({top:620,left:bounds.formLeft,width:bounds.formWidth});
    f.prompt.getBoundingClientRect = () => ({top:632,bottom:684}); f.popup.style = {}; f.popup.offsetHeight = 180;
    f.type('@'); f.reply(listing([entry('test.go')])); await tick();
    assert.equal(f.popup.style.width, `${bounds.expectedWidth}px`); assert.equal(f.popup.style.left, `${bounds.expectedLeft}px`);
  }
});

function previews() {
  const created = [], revoked = [];
  const api = environment({}, {
    createObjectURL(blob) { const url = `blob:local-preview-${created.length + 1}`; created.push({url, blob}); return url; },
    revokeObjectURL(url) { revoked.push(url); },
  });
  return {api, created, revoked};
}
const pngBytes = Uint8Array.from(Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII=', 'base64'));
const raster = () => file('photo.png', pngBytes, 'image/png');
const thumbnails = f => f.nodes['[data-composer-context-items]'].querySelectorAll('.composer-context-thumbnail');

test('ready raster previews use only validated bytes in local blobs and do not affect capture', async () => {
  const p = previews(), f = fixture(p.api);
  f.add(raster()); assert.equal(p.created.length, 0); assert.equal(thumbnails(f).length, 0);
  await tick();
  assert.equal(p.created.length, 1); assert.equal(f.calls.length, 0);
  const {url, blob} = p.created[0], image = thumbnails(f)[0].children[0];
  assert.equal(blob.type, 'image/png'); assert.equal(blob.size, pngBytes.length);
  assert.deepEqual([...new Uint8Array(await blob.arrayBuffer())], [...pngBytes]);
  assert.equal(image.tagName, 'img'); assert.equal(image.src, url); assert.equal(image.alt, '');
  assert.equal(image.width, 32); assert.equal(image.height, 32);
  const before = f.controller.capture('look');
  f.controller.render({editable:false}); f.controller.render({editable:true});
  assert.equal(p.created.length, 1); assert.deepEqual(p.revoked, []);
  assert.equal(f.controller.capture('look').content, before.content);
  assert.equal(before.content.includes('blob:'), false);
  f.nodes['[data-composer-context-items]'].children[0].children.at(-1).click();
  assert.deepEqual(p.revoked, [url]); assert.equal(f.controller.hasAttachments(), false);
  image.dispatch('error'); f.controller.dispose(); assert.deepEqual(p.revoked, [url]);
});

test('preview release follows exact accepted snapshot ownership and preserves later images', async () => {
  const p = previews(), f = fixture(p.api);
  f.add(raster()); await tick(); const snapshot = f.controller.capture('send');
  f.add(raster()); await tick();
  f.controller.render({editable:false}); f.controller.accepted(snapshot);
  assert.deepEqual(p.revoked, [p.created[0].url]); assert.equal(thumbnails(f).length, 1);
  f.controller.accepted(snapshot); assert.equal(p.revoked.length, 1);
  f.controller.dispose(); assert.deepEqual(p.revoked, p.created.map(item => item.url));
});

test('retired ready drafts recreate previews from retained content and fence old callbacks and sends', async () => {
  const p = previews(), f = fixture(p.api);
  f.add(raster()); await tick(); const snapshot = f.controller.capture('send');
  const oldImage = thumbnails(f)[0].children[0], oldCallback = oldImage.events.error[0].handler;
  const next = fixture(p.api);
  assert.equal(p.created.length, 2); assert.deepEqual(p.revoked, [p.created[0].url]);
  assert.equal(next.controller.capture('send').content, snapshot.content);
  oldCallback(); next.controller.accepted(snapshot); f.controller.dispose();
  assert.deepEqual(p.revoked, [p.created[0].url]); assert.equal(next.controller.hasAttachments(), true);
  next.controller.dispose(); next.controller.dispose();
  assert.deepEqual(p.revoked, p.created.map(item => item.url));
});

test('forget and draft eviction drop image data without double-revoking retired previews', async () => {
  const p = previews(), f = fixture(p.api, 'first'); f.add(raster()); await tick();
  const callback = thumbnails(f)[0].children[0].events.error[0].handler;
  p.api.forget('first'); callback(); p.api.forget('first'); f.controller.dispose();
  assert.deepEqual(p.revoked, [p.created[0].url]);
  assert.equal(fixture(p.api, 'first').controller.hasAttachments(), false);
  const retained = fixture(p.api, 'retained'); retained.add(raster()); await tick();
  for (let i = 0; i < 16; i++) fixture(p.api, `eviction:${i}`);
  assert.deepEqual(p.revoked, p.created.map(item => item.url));
  assert.equal(fixture(p.api, 'retained').controller.hasAttachments(), false);
  assert.equal(p.created.length, 2);
});

test('failed or stale image reads never create previews, including oversize images', async () => {
  for (const invalidate of [f => f.controller.dispose(), f => f.api.forget('project:session'), f => f.nodes['[data-composer-context-items]'].children[0].children.at(-1).click()]) {
    const p = previews(), f = fixture(p.api); let resolve;
    f.add({name:'pending.png',type:'image/png',size:pngBytes.length,arrayBuffer:() => new Promise(done => {resolve = done;})});
    invalidate(f); resolve(await raster().arrayBuffer()); await tick();
    assert.equal(p.created.length, 0); assert.deepEqual(p.revoked, []);
  }
  const p = previews(), f = fixture(p.api), huge = new Uint8Array(2 * 1024 * 1024 + 1); huge.set([255,216,255]);
  f.add(file('fake.png','not raster','image/png'), file('huge.jpg', huge)); await tick();
  assert.equal(p.created.length, 0); assert.equal(thumbnails(f).length, 0);
  const valid = new Uint8Array(1024 * 1024 + 1); valid.set(pngBytes);
  const bounded = fixture(p.api, 'bounded'); bounded.add(file('one.png',valid)); await tick();
  bounded.add(file('two.png',valid)); await tick();
  assert.equal(p.created.length, 1); assert.equal(p.created[0].blob.size, valid.length);
});

test('browser preview failures fall back to the filename without invalidating attachment or retry loops', async () => {
  const p = previews(), f = fixture(p.api); f.add(raster()); await tick();
  const stale = thumbnails(f)[0].children[0]; f.controller.render({}); stale.dispatch('error');
  assert.equal(p.revoked.length, 0, 'replaced DOM callback cannot invalidate current preview');
  const thumbnail = thumbnails(f)[0], before = f.controller.capture('send'); thumbnail.children[0].dispatch('error');
  assert.equal(thumbnail.hidden, true); assert.deepEqual(p.revoked, [p.created[0].url]);
  assert.equal(f.controller.capture('send').content, before.content);
  assert.equal(f.controller.capture('send').revision, before.revision); assert.deepEqual(f.errors, []);
  f.controller.render({}); assert.equal(thumbnails(f).length, 0); assert.equal(p.created.length, 1);
  f.controller.accepted(before); assert.equal(f.controller.hasAttachments(), false); assert.equal(p.revoked.length, 1);
  const unsupported = fixture(environment({}, {createObjectURL() {throw new Error('unavailable');}}));
  unsupported.add(raster()); await tick(); assert.equal(thumbnails(unsupported).length, 0);
  assert.equal(JSON.parse(unsupported.controller.capture('send').content)[1].type, 'image');
});

test('compact notices omit vision guidance for text and retain full privacy, local errors and read-only recovery', async () => {
  const p = previews(), f = fixture(p.api), notice = f.nodes['[data-composer-context-status]'];
  assert.equal(notice.hidden, true); assert.equal(notice.title, '');
  f.add(file('notes.txt','hello')); await tick();
  assert.equal(notice.textContent, 'On Send: shared with provider and saved in chat.');
  assert.match(notice.title, /Text and image contents will be sent to the provider and persisted in saved conversation history when you send/);
  assert.equal(p.created.length, 0); assert.equal(thumbnails(f).length, 0);
  f.add({name:'huge',size:2 * 1024 * 1024 + 1});
  f.controller.render({editable:false});
  assert.equal(notice.hidden, false); assert.match(notice.textContent, /Attachments being read must total at most 2 MiB/);
  assert.match(notice.textContent, /Attachments are kept; finish or cancel the current operation to change them\./);
  assert.equal(notice.textContent.includes('vision'), false);
  f.controller.render({editable:true}); f.add(raster()); await tick();
  assert.equal(notice.textContent, 'On Send: shared with provider and saved in chat. Images need a vision-capable model.');
});

function pngHeader(width, height) {
  const data = Buffer.from(pngBytes); data.writeUInt32BE(width, 16); data.writeUInt32BE(height, 20); return data;
}
function gifHeader(width, height) {
  const data = Buffer.from('R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7', 'base64');
  data.writeUInt16LE(width, 6); data.writeUInt16LE(height, 8); return data;
}
function jpegHeader(width, height, marker = 0xc0) {
  // SOI, bounded APP segment, then a one-component SOF header. No pixel decode.
  const data = Buffer.from([255,216,255,224,0,4,0,0,255,marker,0,11,8,0,0,0,0,1,1,0x11,0]);
  data.writeUInt16BE(height, 13); data.writeUInt16BE(width, 15); return data;
}
function webpHeader(kind, width, height) {
  const payload = Buffer.alloc(kind === 'VP8L' ? 5 : 10);
  if (kind === 'VP8X') { payload.writeUIntLE(width - 1, 4, 3); payload.writeUIntLE(height - 1, 7, 3); }
  else if (kind === 'VP8L') { payload[0] = 0x2f; payload.writeUInt32LE((width - 1) + (height - 1) * 16384, 1); }
  else { payload.set([0,0,0,0x9d,1,0x2a]); payload.writeUInt16LE(width, 6); payload.writeUInt16LE(height, 8); }
  const data = Buffer.alloc(20 + payload.length + payload.length % 2);
  data.write('RIFF', 0); data.writeUInt32LE(data.length - 8, 4); data.write('WEBP' + kind, 8);
  data.writeUInt32LE(payload.length, 16); payload.copy(data, 20); return data;
}
const dimensionFormats = [
  ['png', pngHeader], ['gif', gifHeader], ['jpeg', jpegHeader],
  ['webp', (w,h) => webpHeader('VP8X',w,h)], ['webp', (w,h) => webpHeader('VP8L',w,h)], ['webp', (w,h) => webpHeader('VP8 ',w,h)],
];

test('preview headers admit bounded PNG/GIF/JPEG and each WebP dimension encoding without changing protocol content', async () => {
  for (const [extension, header] of dimensionFormats) {
    const p = previews(), f = fixture(p.api), data = header(123,456);
    f.add(file(`image.${extension}`, data)); assert.equal(p.created.length, 0);
    await tick(); assert.equal(p.created.length, 1, extension); assert.equal(thumbnails(f).length, 1);
    assert.equal(p.created[0].blob.type, `image/${extension}`);
    assert.equal(JSON.parse(f.controller.capture('send').content)[1].data, data.toString('base64'));
  }
  for (const marker of [0xc1, 0xc2]) {
    const p = previews(), f = fixture(p.api); f.add(file('image.jpg', jpegHeader(1,1,marker))); await tick();
    assert.equal(p.created.length, 1);
  }
});

test('axis and pixel header limits suppress previews before browser decoding, without blocking attachment capture', async () => {
  for (const [extension, header] of dimensionFormats) {
    // Every encoding can represent this pixel bomb, despite its tiny byte size.
    const p = previews(), f = fixture(p.api); f.add(file(`huge.${extension}`, header(10000,10000))); await tick();
    assert.equal(p.created.length, 0, extension); assert.equal(thumbnails(f).length, 0);
    assert.equal(JSON.parse(f.controller.capture('send').content)[1].type, 'image');
    assert.equal(f.controller.pending(), false); assert.deepEqual(f.errors, []);
  }
  for (const [extension, header] of dimensionFormats.slice(0,4)) {
    for (const [width,height] of [[16385,1], [1,16385], [0,1], [1,0]]) {
      if (extension === 'webp' && (!width || !height)) continue; // WebP dimensions store N-1.
      const p = previews(), f = fixture(p.api); f.add(file(`huge.${extension}`, header(width,height))); await tick();
      assert.equal(p.created.length, 0, `${extension} ${width}x${height}`);
      assert.equal(f.controller.capture('send').hasContent, true);
    }
  }
  for (const [width,height] of [[16384,1], [1,16384], [8000,5000]]) {
    const p = previews(), f = fixture(p.api); f.add(file('boundary.png', pngHeader(width,height))); await tick();
    assert.equal(p.created.length, 1, `${width}x${height}`);
  }
});

test('tiny, truncated and malformed dimension headers never create URLs or silently drop attachments', async () => {
  const badPNGChunk = pngHeader(1,1); badPNGChunk[15] = 0;
  const badJPEGLength = jpegHeader(1,1); badJPEGLength[11] = 2;
  const badJPEGComponent = jpegHeader(1,1); badJPEGComponent[17] = 4;
  const badJPEGPrefix = jpegHeader(1,1); badJPEGPrefix[5] = 255;
  const scanBeforeSOF = jpegHeader(1,1); scanBeforeSOF[3] = 0xda;
  const badWebPSize = webpHeader('VP8X',1,1); badWebPSize.writeUInt32LE(0xffffffff,16);
  const badWebPVersion = webpHeader('VP8L',1,1); badWebPVersion[24] |= 0x20;
  const badWebPStart = webpHeader('VP8 ',1,1); badWebPStart[23] = 0;
  const interframeWebP = webpHeader('VP8 ',1,1); interframeWebP[20] = 1;
  const markerFlood = Buffer.concat([Buffer.from([255,216]), Buffer.from(Array.from({length:1024}, () => [255,224,0,2]).flat()), jpegHeader(1,1).subarray(8)]);
  const cases = [
    pngBytes.subarray(0,8), pngBytes.subarray(0,32), badPNGChunk,
    gifHeader(1,1).subarray(0,6), gifHeader(1,1).subarray(0,12),
    Buffer.from([255,216,255]), jpegHeader(1,1).subarray(0,20), badJPEGLength, badJPEGComponent, badJPEGPrefix, scanBeforeSOF, markerFlood,
    Buffer.from('RIFF0000WEBP'), webpHeader('VP8X',1,1).subarray(0,29), webpHeader('VP8L',1,1).subarray(0,25),
    webpHeader('VP8 ',1,1).subarray(0,29), badWebPSize, badWebPVersion, badWebPStart, interframeWebP,
  ];
  for (const [index,data] of cases.entries()) {
    const p = previews(), f = fixture(p.api); f.add(file(`invalid-${index}`,data)); await tick();
    assert.equal(p.created.length, 0, `case ${index}`); assert.equal(thumbnails(f).length, 0);
    const capture = f.controller.capture('send');
    assert.equal(JSON.parse(capture.content)[1].type, 'image', `case ${index}`);
    f.controller.render({}); assert.equal(p.created.length, 0); assert.equal(f.controller.capture('send').revision, capture.revision);
    assert.deepEqual(f.errors, []);
  }
});
