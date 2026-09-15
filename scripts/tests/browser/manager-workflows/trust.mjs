// Native remembered-trust presentation and revocation. The earlier workflow
// explicitly checks the real activation checkbox; no fixture grants consent.
export async function trustChecks({click, evaluate, wait, check, snapshot, callCount, requests, send}) {
  const q = selector => `document.querySelector(${JSON.stringify(selector)})`;
  const before = await snapshot(), calls = await callCount();
  const opens = () => requests.filter(r => r.method === "POST" && r.path.endsWith("/runtime/open")).length;
  const initialOpens = opens();
  const close = async () => {
    await click("[data-runtime-close]");
    await click("[data-runtime-close-confirm]");
    await wait(`!!${q('[data-runtime-open]')}`, "explicit runtime close returns to activation");
  };
  await close();
  await wait(`${q('[data-runtime-open] input[name=confirm]')}?.value==='trusted'`, "remembered consent shown after runtime close");
  await send("Page.reload", {ignoreCache: true});
  await wait(`${q('[data-runtime-open] input[name=confirm]')}?.value==='trusted'`, "remembered consent survives page reload");
  check(await evaluate(`!${q('[data-runtime-open] input[type=checkbox][name=confirm]')} && ${q('[data-runtime-open] input[name=enable_skills]')}?.checked === false && !!${q('.activation-trusted')} && !${q('.activation-boundary')}`), "Trusted project shows compact Start/Resume without repeated trust confirmation; skills remain unchecked");
  check(await callCount() === calls && opens() === initialOpens, "Closing/reloading remembered trust never starts a worker or provider");
  // Explicitly choose the saved conversation through its current navigation
  // surface. The reload may already show that session, so wait for this read's
  // committed workspace replacement, not merely a matching old hidden input.
  // Saved conversations now live in the production sidebar, not the retired
  // in-conversation catalog list. Open it on narrow screens before selecting.
  if (await evaluate('document.querySelector("#project-navigation").inert'))
    await click('[data-nav-toggle]');
  const savedLink = `[data-sidebar-project="${before.project_id}"] a[data-shell-session-open][href*="session=${before.session_id}"]`;
  await wait(`!!${q(savedLink)}`, 'saved conversation appears in the authoritative sidebar inventory');
  await evaluate(`window.trustPreviousWorkspace = document.querySelector('#workspace'); void 0`);
  await click(savedLink);
  await wait(`${q('#workspace')}!==window.trustPreviousWorkspace && ${q('[data-react-page="workspace-cold"]')}?.dataset.reactMounted==='true' && ${q('[data-runtime-open] input[name=session_id]')}?.value===${JSON.stringify(before.session_id)}`, "explicit saved conversation selected for compact Resume");
  await evaluate('delete window.trustPreviousWorkspace');
  await click('[data-runtime-open] button[type=submit]');
  await wait(`${q('#live-status')}?.textContent==='Ready' && ${q('#live-connection')}?.textContent==='Live'`, "explicit compact Resume starts worker");
  const resumed = await snapshot();
  check(resumed.session_id === before.session_id && resumed.permission_mode === before.permission_mode && resumed.instance_id !== before.instance_id, `Remembered trust resumes exact selected session without changing its saved tool policy (${before.session_id === resumed.session_id ? "same session" : "different session"}; ${before.permission_mode} → ${resumed.permission_mode}; new instance ${before.instance_id !== resumed.instance_id})`);
  check(opens() === initialOpens + 1 && await callCount() === calls, "Compact Resume sends one explicit activation and no provider prompt");
  await close();
  await click('[data-runtime-open] .activation-model-help > summary');
  await wait(`${q('[data-runtime-open] .activation-model-help')}?.open`, 'startup settings disclosure exposes remembered-trust management');
  await click('[data-settings-open="workspaces"]');
  await wait(`${q('#settings-dialog')}?.open && !${q('#settings-workspaces')}.hidden`, "manage trust opens Workspaces settings directly");
  await click('[data-project-trust-revoke] button');
  await wait(`!!${q('[data-runtime-open] input[type=checkbox]')}`, "explicit Forget trust restores unchecked confirmation");
  check(await evaluate(`!${q('[data-runtime-open] input[type=checkbox]')}.checked && ${q('[data-runtime-open] input[type=checkbox]')}.required`), "Forgotten project requires fresh explicit trust; no prechecked bypass");
  check(opens() === initialOpens + 1 && await callCount() === calls, "Forgetting trust does not activate or execute work");
}
