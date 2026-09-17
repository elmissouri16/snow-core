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
  check(await evaluate(`!${q('[data-runtime-open] input[type=checkbox][name=confirm]')} && !${q('[data-runtime-open] input[name=enable_skills]')} && ${q('[data-runtime-open] [data-skills-startup-policy]')}?.textContent.includes('enabled for this workspace') && !!${q('.activation-trusted')} && !${q('.activation-boundary')}`), "Trusted project shows compact Start/Resume, inherits enabled skills from Settings, and repeats no trust or skills checkbox");
  check(await callCount() === calls && opens() === initialOpens, "Closing/reloading remembered trust never starts a worker or provider");
  // A deliberate same-tab saved-conversation selection is the activation
  // action once workspace trust is remembered. Direct URLs and reloads above
  // remain passive. Saved conversations live in the production sidebar; open
  // it on narrow screens before selecting.
  if (await evaluate('document.querySelector("#project-navigation").inert'))
    await click('[data-nav-toggle]');
  const savedLink = `[data-sidebar-project="${before.project_id}"] a[data-shell-session-open][href*="session=${before.session_id}"]`;
  await wait(`!!${q(savedLink)}`, 'saved conversation appears in the authoritative sidebar inventory');
  await click(savedLink);
  await wait(`${q('#live-status')}?.textContent==='Ready' && ${q('#live-connection')}?.textContent==='Live'`, "explicit trusted saved-conversation selection resumes directly into the live conversation");
  const resumed = await snapshot();
  check(resumed.session_id === before.session_id && resumed.permission_mode === before.permission_mode && resumed.instance_id !== before.instance_id, `Remembered trust resumes exact selected session without changing its saved tool policy (${before.session_id === resumed.session_id ? "same session" : "different session"}; ${before.permission_mode} → ${resumed.permission_mode}; new instance ${before.instance_id !== resumed.instance_id})`);
  check(opens() === initialOpens + 1 && await callCount() === calls, "Direct trusted entry sends one explicit activation and no provider prompt");
  await close();
  await click('[data-runtime-open] .activation-model-help > summary');
  await wait(`${q('[data-runtime-open] .activation-model-help')}?.open`, 'startup settings disclosure exposes remembered-trust management');
  await click('[data-settings-open="workspaces"]');
  await wait(`${q('#settings-dialog')}?.open && !${q('#settings-workspaces')}.hidden`, "manage trust opens Workspaces settings directly");
  await click('[data-project-trust-revoke] button');
  await wait(`!!${q('[data-runtime-open] input[type=checkbox]')}`, "explicit Forget trust restores unchecked confirmation");
  check(await evaluate(`!${q('[data-runtime-open] input[type=checkbox]')}.checked && ${q('[data-runtime-open] input[type=checkbox]')}.required`), "Forgotten project requires fresh explicit trust; no prechecked bypass");
  if (await evaluate('document.querySelector("#project-navigation").inert'))
    await click('[data-nav-toggle]');
  await click(savedLink);
  await wait(`!!${q('[data-runtime-open] input[type=checkbox][name=confirm]')}`, "untrusted saved-conversation selection remains at the trust gate");
  check(opens() === initialOpens + 1 && await callCount() === calls, "Forgetting trust and selecting saved history do not activate or execute work");
}
