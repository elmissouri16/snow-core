// Real HTMX navigation over exported production markup. No worker or provider.
export async function sidebarChecks({state, evaluate, wait, click, key, navigate}) {
  const results = [], failures = [];
  const check = (value, label) => { results.push(label); if (!value) failures.push(label); };
  try {
    await navigate({streaming: true});
    await evaluate(`window.sidebarRevision=0;window.sidebarMutations=0;window.sidebarObserver=new MutationObserver(records=>sidebarMutations+=records.length);sidebarObserver.observe(document.querySelector('#project-navigation'),{subtree:true,childList:true,attributes:true});const owner=window.SnowConversation;window.SnowConversation={...owner,render(snapshot,...args){const result=owner.render(snapshot,...args);window.sidebarRevision=snapshot.revision;return result}};`);
    state.update({}); state.push();
    await wait(`sidebarRevision === ${state.snapshot.revision}`, 'unchanged sidebar snapshot delivered');
    check(await evaluate('sidebarMutations===0'), 'Unchanged runtime update does not replace sidebar labels or rewrite row states');
    await evaluate('sidebarObserver.disconnect()');
    await click('[data-shell-live-session] [data-shell-session-menu]');
    await click('.snow-menu .snow-menu-row');
    await wait(`document.querySelector('#workflow-rename-dialog').open`, 'sidebar rename dialog');
    await key('Escape','Escape',27);
    check(await evaluate(`document.activeElement===document.querySelector('[data-shell-live-session] [data-shell-session-menu]')`), 'Closing sidebar Rename returns focus to its row, not the conversation header');

    // Navigate through the existing HTMX owner into the long production catalog.
    await evaluate(`window.htmx.ajax('GET','/sidebar.html',{target:'#workspace',swap:'outerHTML',select:'#workspace'})`);
    await wait(`document.querySelectorAll('[data-sidebar-project]').length===100`, 'long sidebar mounted');
    await click('[data-sidebar-search-toggle]');
    await evaluate(`document.querySelector('[data-sidebar-search]').value='Workspace';document.querySelector('[data-sidebar-search]').dispatchEvent(new Event('input',{bubbles:true}));const a=document.querySelector('[data-sidebar-project="00000000-0000-4000-8000-000000000150"] .project-link');a.scrollIntoView({block:'center'});a.focus({preventScroll:true});window.sidebarScroll=document.querySelector('.project-tree').scrollTop;window.sidebarSwaps=0;window.sidebarSettles=0;document.addEventListener('htmx:afterSwap',e=>{if(e.detail.target?.id==='workspace')sidebarSwaps++});document.addEventListener('htmx:afterSettle',e=>{if(e.detail.target?.id==='workspace')sidebarSettles++})`);
    await click('[data-sidebar-project="00000000-0000-4000-8000-000000000150"] .project-link');
    await wait('sidebarSwaps===1 && sidebarSettles>=1', 'sidebar navigation completed');
    check(await evaluate(`document.activeElement.closest('[data-sidebar-project]')?.dataset.sidebarProject==='00000000-0000-4000-8000-000000000150'`), 'Desktop sidebar navigation keeps focus on the corresponding destination row');
    check(await evaluate(`!document.querySelector('#sidebar-search').hidden && document.querySelector('[data-sidebar-search]').value==='Workspace'`), 'Workspace navigation retains the open search and filter');
    check(await evaluate(`Math.abs(document.querySelector('.project-tree').scrollTop-sidebarScroll)<2`), 'Workspace navigation preserves the list reading position');

    state.navigation = []; state.holdNavigation = true;
    await click('[data-sidebar-project="00000000-0000-4000-8000-000000000151"] .project-link');
    for (let i=0; i<100 && !state.releaseNavigation; i++) await new Promise(resolve=>setTimeout(resolve,20));
    if (!state.releaseNavigation) throw new Error('Sidebar navigation was not held');
    await click('[data-sidebar-project="00000000-0000-4000-8000-000000000152"] .project-link');
    await wait(`new URL(location.href).searchParams.get('project')==='00000000-0000-4000-8000-000000000152' && sidebarSettles>=2`, 'newest sidebar selection displayed');
    const swaps = await evaluate('sidebarSwaps');
    state.releaseNavigation(); state.releaseNavigation=null;
    await new Promise(resolve=>setTimeout(resolve,100));
    check(state.navigation[0]?.aborted === true, 'New sidebar selection cancels the earlier read, not a runtime operation');
    check(await evaluate(`sidebarSwaps===${swaps} && new URL(location.href).searchParams.get('project')==='00000000-0000-4000-8000-000000000152'`), 'Late old navigation cannot replace the latest selection or history URL');

    state.holdNavigation = true;
    await click('[data-sidebar-project="00000000-0000-4000-8000-000000000153"] .project-link');
    for (let i=0; i<100 && !state.releaseNavigation; i++) await new Promise(resolve=>setTimeout(resolve,20));
    if (!state.releaseNavigation) throw new Error('Failed navigation was not held');
    await evaluate(`document.querySelector('#home-prompt').focus()`);
    state.releaseNavigation(); state.releaseNavigation=null;
    await wait(`new URL(location.href).searchParams.get('project')==='00000000-0000-4000-8000-000000000153'`, 'delayed navigation completed');
    check(await evaluate(`!document.activeElement.closest('#project-navigation')`), 'Completing navigation does not pull focus back after the user leaves the sidebar');

    // A rejected read must not dispose the still-mounted live conversation.
    await navigate({streaming: true});
    const opens=state.streamOpens; state.holdNavigation=true;
    await evaluate(`void window.htmx.ajax('GET','/',{target:'#workspace',swap:'outerHTML'}).catch(()=>{})`);
    for (let i=0; i<100 && !state.releaseNavigation; i++) await new Promise(resolve=>setTimeout(resolve,20));
    if (!state.releaseNavigation) throw new Error('Rejected navigation was not held');
    state.releaseNavigation(503); state.releaseNavigation=null;
    await wait(`!document.querySelector('#connection-error').hidden`, 'navigation failure remains visible');
    state.update({session_name:'Still connected'}); state.push();
    await wait(`document.querySelector('[data-live-title]')?.textContent==='Still connected'`, 'failed read preserves the mounted live subscription');
    check(state.streamOpens===opens && await evaluate(`!document.querySelector('#live-send').disabled`), 'Rejected navigation does not retire the live session or disable its composer');
    await evaluate(`window.htmx.ajax('GET','/',{target:'#workspace',swap:'outerHTML',select:'#workspace'})`);
    await wait(`document.querySelectorAll('[data-sidebar-project]').length===100`, 'content navigation fallback');
    check(await evaluate(`document.activeElement.id==='workspace-content'`), 'Non-sidebar navigation still focuses the content region');
    check(state.errors.length===0, 'Sidebar checks use bounded local transport without invalid runtime requests');
  } finally {
    state.releaseNavigation?.(); state.releaseNavigation=null; state.holdNavigation=false;
  }
  return {results, failures};
}
