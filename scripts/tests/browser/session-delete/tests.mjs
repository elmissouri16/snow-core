// Every request outcome is released by the test; no provider, polling sleep or retry.
export async function deletionTests(h) {
  const {run, evaluate: e, check, key} = h;
  for (const supported of ['false', 'missing', 'string-true']) {
    await run(`capability ${supported} fails closed`, {supported}, async () => {
      await check('document.querySelectorAll("[data-shell-session]").length === 4 && !document.querySelector("[data-session-delete]")', 'Inventory stays browsable without deletion capability');
      await check('f.posts().length === 0 && f.navigation.length === 0', 'Inventory causes no mutation or navigation');
    });
  }
  for (const activeField of ['missing', 'number']) {
    await run(`malformed active-session authority ${activeField}`, {activeField}, async () => {
      await check('(!f.button() || f.button().hidden && f.button().disabled) && !!f.row()', 'Capability requires an explicit string active_session_id');
    });
  }
  await run('supported inventory uses only shared ellipsis menus and active guards', {live: '1'}, async () => {
    await check('document.querySelectorAll("[data-shell-session-menu]").length === 4 && !document.querySelector("[data-shell-session] [data-session-delete]")', 'Every supported row has one shared launcher and no inline Delete');
    await check('[...document.querySelectorAll("[data-shell-session]")].every(row => row.querySelectorAll("button").length === 1 && row.querySelector("button").textContent === "⋯" && !row.querySelector("button svg"))', 'No trash icon or extra button is added to any session row');
    await check('!f.button("a", "active").disabled && !f.button().disabled && !f.button("b").disabled', 'Active session keeps an available shared menu for Rename');
    await check('f.button().dataset.instance === "worker-a" && f.button("b").dataset.instance === ""', 'Per-project instance ownership stays on the ellipsis');
    await e('window.menuReadBaseline = f.requests.length; await f.menu("a", "active")');
    await check('JSON.stringify([...f.$(".snow-menu").querySelectorAll("[role=menuitem]")].map(node => node.textContent)) === JSON.stringify(["Rename", "Delete session"])', 'Current Rename and Delete share the same popup');
    await check('f.deleteItem().disabled && f.deleteItem().title.includes("Switch")', 'Only Delete is disabled for the active session with an explanation');
    await e('f.deleteItem().click(); f.deleteItem().dispatchEvent(new MouseEvent("click", {bubbles: true}))');
    await check('!f.$("#session-delete-dialog") && f.posts().length === 0 && f.requests.length === menuReadBaseline', 'Active Delete cannot open a confirmation or request, even via synthetic click');
    await e('[...f.$(".snow-menu").querySelectorAll("[role=menuitem]")].find(node => node.textContent === "Rename").click()');
    await check('f.renames.length === 1 && f.renames[0] === f.button("a", "active") && !f.$(".snow-menu") && f.requests.length === menuReadBaseline', 'Existing Rename still forwards its actual ellipsis without a deletion request');
    await e('await f.menu()');
    await check('!!f.deleteItem() && !f.deleteItem().disabled && f.deleteItem().closest(".snow-menu").parentElement === document.body && f.requests.length === menuReadBaseline', 'Saved-session Delete exists only inside the shared body popup; opening is request-free');
    await key('Escape');
    await check('!f.$(".snow-menu") && document.activeElement === f.button() && f.requests.length === menuReadBaseline && !f.$("#session-delete-dialog")', 'Popup Escape returns focus to ellipsis without confirmation, reads or mutation');
    await e('await f.open()');
    await check('f.$("#session-delete-dialog")?.open && f.$("[data-delete-name]").textContent === f.name', 'Confirmation names the exact saved conversation');
    await check('!f.$("[data-delete-name] img") && f.button().getAttribute("aria-label").includes(f.name)', 'Untrusted session title remains text and accessible name');
    await check('/permanently/i.test(f.$("#session-delete-warning").textContent) && /cannot be undone/i.test(f.$("#session-delete-warning").textContent)', 'Confirmation explicitly warns of permanent deletion');
    await check('!f.$("[data-delete-confirm]").checked && f.$("[data-delete-submit]").disabled && document.activeElement === f.$("[data-delete-cancel]")', 'Every open starts unchecked, disabled and focused on Cancel');
    // Even synthetic dispatch cannot bypass the authoritative checkbox check.
    await e('f.$("[data-delete-submit]").dispatchEvent(new MouseEvent("click", {bubbles: true}))');
    await check('f.posts().length === 0', 'Unchecked confirmation never sends a request');
    await e('f.$("[data-delete-cancel]").click()');
    await check('!f.$("#session-delete-dialog")?.open && document.activeElement === f.button() && f.posts().length === 0', 'Cancel restores the exact trigger without mutation');
    await e('await f.open(); f.$("[data-delete-confirm]").click(); window.staleSubmit = f.$("[data-delete-submit]")');
    await key('Escape');
    await e('f.until(() => !f.$("#session-delete-dialog")?.open, "native Escape")');
    await check('document.activeElement === f.button() && f.posts().length === 0', 'Native Escape cancels checked confirmation and restores focus');
    await e('await f.open()');
    await check('!f.$("[data-delete-confirm]").checked && f.$("[data-delete-submit]").disabled', 'Reopening never retains prior acknowledgement');
  });
  await run('unavailable inventory disables deletion', {available: 'false'}, async () => {
    await e('await f.menu()');
    await check('!!f.button() && !!f.deleteItem() && f.deleteItem().disabled', 'Unavailable target has no enabled Delete action');
  });
  await run('revoked capability leaves no usable deletion action', {}, async () => {
    await e('f.button().focus(); f.inventories.a.delete_supported = false; SnowSidebarSessions.invalidate(f.project("a")); await f.idle()');
    await check('(!f.button() || f.button().hidden || f.button().disabled) && document.activeElement === f.row().querySelector("a") && f.posts().length === 0', 'Capability withdrawal restores focus to surviving row link and hides unusable menu');
  });
  await run('capability withdrawal closes its open popup and restores surviving row focus', {}, async () => {
    await e('await f.menu(); window.retiredAction = f.deleteItem(); f.inventories.a.delete_supported = false; SnowSidebarSessions.invalidate(f.project("a")); await f.idle()');
    await check('!f.$(".snow-menu") && f.button().hidden && f.button().disabled && document.activeElement === f.row().querySelector("a")', 'Revoking the popup owner closes its menu, hides the empty ellipsis and restores row-link focus');
    await e('retiredAction.dispatchEvent(new MouseEvent("click", {bubbles: true}))');
    await check('!f.$("#session-delete-dialog")?.open && f.posts().length === 0 && f.navigation.length === 0', 'Detached popup action cannot revive the revoked deletion');
  });
  await run('capability withdrawal preserves the open current Rename menu', {live: '1'}, async () => {
    await e('await f.menu("a", "active"); window.keptMenu = f.$(".snow-menu"); window.keptRename = [...keptMenu.querySelectorAll("[role=menuitem]")].find(node => node.textContent === "Rename"); f.inventories.a.delete_supported = false; SnowSidebarSessions.invalidate(f.project("a")); await f.idle()');
    await check('f.$(".snow-menu") === keptMenu && !f.button("a", "active").hidden && !f.button("a", "active").disabled && !keptRename.disabled && document.activeElement === keptRename', 'Losing Delete does not hide the shared current launcher, close Rename or steal its focus');
    await e('keptRename.click()');
    await check('f.renames.length === 1 && f.renames[0] === f.button("a", "active") && f.posts().length === 0', 'Preserved Rename still forwards its exact launcher after capability revocation');
    await e('await f.menu("a", "active")');
    await check('JSON.stringify([...f.$(".snow-menu").querySelectorAll("[role=menuitem]")].map(node => node.textContent)) === JSON.stringify(["Rename"])', 'Reopened current menu reflects revoked Delete capability and retains only Rename');
  });
  await run('capability withdrawal cannot close another workspace popup', {}, async () => {
    await e('await f.menu("b"); window.keptMenu = f.$(".snow-menu"); window.keptAction = f.deleteItem(); f.inventories.a.delete_supported = false; SnowSidebarSessions.invalidate(f.project("a")); await f.idle()');
    await check('f.$(".snow-menu") === keptMenu && document.activeElement === keptAction && !keptAction.disabled && f.button().hidden', 'Hiding retired workspace launchers preserves another workspace menu and focus');
    await key('Escape');
    await check('document.activeElement === f.button("b") && f.posts().length === 0 && f.navigation.length === 0', 'Unaffected menu still dismisses to its own ellipsis without mutation');
  });
  await run('unsupported deletion preserves existing current Rename menu', {live: '1', supported: 'false'}, async () => {
    await e('await f.menu("a", "active")');
    await check('JSON.stringify([...f.$(".snow-menu").querySelectorAll("[role=menuitem]")].map(node => node.textContent)) === JSON.stringify(["Rename"]) && !f.deleteItem()', 'Capability false adds no Delete while preserving current Rename');
    await check('f.row("a", "active").querySelectorAll("button").length === 1 && f.posts().length === 0', 'Existing Rename ellipsis is reused without extra row actions');
  });
  await run('exact single immutable request, target-only success and event metadata', {live: '1'}, async () => {
    await e('window.keptRow = f.row("a", "saved-two"); window.otherRow = f.row("b"); window.trigger = f.button(); trigger.focus()');
    // A native user gesture matters: CloseWatcher can make Escape's cancel event
    // noncancelable when a dialog was opened only by untrusted script clicks.
    await key('Enter');
    await check('document.activeElement === f.deleteItem()', 'Trusted ellipsis opening focuses the Delete menu item');
    await key('Enter');
    await e('await f.until(() => f.$("#session-delete-dialog")?.open, "trusted opening"); await f.confirm(); trigger.dataset.project = "b"; trigger.dataset.session = "saved-two"; f.$("[data-delete-submit]").dispatchEvent(new MouseEvent("click", {bubbles: true})); f.$("[data-delete-cancel]").click()');
    await check('f.posts().length === 1 && f.posts()[0].url === "/projects/00000000-0000-4000-8000-000000000001/sessions/saved-one/delete"', 'Double dispatch submits captured project/session exactly once');
    await check('JSON.stringify([...new URLSearchParams(f.posts()[0].body)]) === JSON.stringify([["csrf", "fixture-csrf-not-a-secret"], ["confirm", "delete"], ["instance_id", "worker-a"]])', 'POST has only CSRF, explicit delete acknowledgement and captured instance');
    await check('f.posts()[0].credentials === "same-origin" && f.posts()[0].headers.Accept === "application/json" && f.posts()[0].headers["Content-Type"] === "application/x-www-form-urlencoded"', 'POST uses expected credential and media contract');
    await check('f.$("[data-delete-cancel]").disabled && f.$("[data-delete-confirm]").disabled && f.$("[data-delete-submit]").disabled', 'In-flight controls cannot cancel or repeat admitted deletion');
    await key('Escape');
    await check('f.$("#session-delete-dialog")?.open && f.posts().length === 1', 'Native Escape cannot disguise an in-flight outcome');
    await e('f.succeed(); await f.succeeded()');
    await check('!f.row() && f.row("a", "saved-two") === keptRow && f.row("b") === otherRow && !!f.row("a", "active")', 'Only exact project/session row disappears; other identities survive');
    await check('JSON.stringify(f.events) === JSON.stringify([{project: "00000000-0000-4000-8000-000000000001", session: "saved-one", instance: "worker-a"}])', 'Deletion event carries only exact ownership metadata');
    await check('f.$("#fixture-draft").value === "Unrelated unsent draft" && f.navigation.length === 0', 'Unrelated draft and live workspace remain untouched');
    await check('document.activeElement === f.$("[data-sidebar-project=a] [data-workspace-toggle]")', 'Removed trigger falls back to its workspace disclosure');
    await check('f.posts().length === 1 && f.deadlines.size === 0', 'Receipt completes once and clears the deadline');
  });
  await run('viewed cold target opens new state, never activates or sends', {viewed: 'saved-one'}, async () => {
    await e('await f.open(); await f.confirm(); f.succeed(); await f.succeeded()');
    await check('f.posts().length === 1 && new URLSearchParams(f.posts()[0].body).get("instance_id") === ""', 'Cold deletion explicitly carries empty instance ownership');
    await check('JSON.stringify(f.navigation) === JSON.stringify([{method: "GET", url: "/?view=projects&project=00000000-0000-4000-8000-000000000001&new=1", target: "#workspace", swap: "outerHTML", source: "00000000-0000-4000-8000-000000000001", push: "/?view=projects&project=00000000-0000-4000-8000-000000000001&new=1", sync: "#project-navigation:replace"}])', 'Cold success delegates empty-state navigation through the real New anchor contract');
  });
  const invalidReceipts = [
    ['project', {project_id: '00000000-0000-4000-8000-000000000002'}], ['session', {session_id: 'saved-two'}],
    ['instance', {instance_id: 'replaced-worker'}], ['missing instance', {instance_id: undefined}],
    ['deleted false', {deleted: false}], ['deleted string', {deleted: 'true'}]
  ];
  for (const [label, patch] of invalidReceipts) {
    await run(`wrong ${label} receipt has no false success or retry`, {live: '1'}, async () => {
      const receipt = label === 'missing instance' ? 'const receipt = f.receipt(); delete receipt.instance_id; f.reply(receipt)' : `f.reply(f.receipt(${JSON.stringify(patch)}))`;
      await e(`await f.open(); await f.confirm(); ${receipt}`);
      await e('await f.failed()');
      await assertFailure();
    });
  }
  for (const status of [400, 401, 403, 404, 409, 500, 503]) {
    await run(`HTTP ${status} preserves uncertainty without replay`, {}, async () => {
      await e(`await f.open(); await f.confirm(); f.reply("<img src=x onerror=alert(1)> Proxy failure", ${status}, "text/html"); await f.failed()`);
      await check('!f.$("[data-delete-error] img") && !f.$("[data-delete-error]").textContent.includes("Proxy failure")', 'Proxy HTML is neither rendered nor exposed as application error');
      await assertFailure();
    });
  }
  for (const [label, action] of [
    ['timeout', 'f.timeout()'],
    ['network disconnect', 'f.pending.at(-1).reject(new TypeError("Fictional disconnect"))'],
    ['malformed JSON', 'f.reply("{not-json")'],
    ['oversized response', 'f.reply("x".repeat(65537))'],
    ['non-UTF-8 response', 'f.pending.at(-1).resolve(new Response(new Uint8Array([0xff]), {headers: {"Content-Type": "application/json"}}))']
  ]) {
    await run(`${label} has no retry or false success`, {}, async () => {
      await e(`await f.open(); await f.confirm(); ${action}; await f.failed()`);
      await assertFailure();
      if (label === 'timeout') await check('f.posts()[0].aborted', 'Controlled deadline reaches the actual request AbortSignal');
    });
  }
  await run('fixed application conflict is displayed as text', {}, async () => {
    await e('await f.open(); await f.confirm(); f.reply("Session is currently active", 409, "text/plain; charset=utf-8"); await f.failed()');
    await check('f.$("[data-delete-error]").textContent.includes("Session is currently active")', 'Application conflict remains actionable without retry');
    await assertFailure();
  });
  for (const change of ['root', 'project', 'session', 'instance', 'active', 'capability', 'csrf']) {
    await run(`stale ${change} confirmation cannot mutate`, {live: '1'}, async () => {
      await e('await f.open(); f.$("[data-delete-confirm]").click(); window.staleSubmit = f.$("[data-delete-submit]")');
      const mutate = {
        root: 'await f.replaceRoot()',
        project: 'f.button().dataset.project = "b"',
        session: 'f.button().dataset.session = "saved-two"',
        instance: 'f.button().dataset.instance = "new-worker"',
        active: 'f.button().dataset.deleteActive = "true"',
        capability: 'f.inventories.a.delete_supported = false; SnowSidebarSessions.invalidate(f.project("a")); await f.idle()',
        csrf: 'SnowShell.snapshot.bootstrap.csrf = ""'
      }[change];
      await e(`${mutate}; staleSubmit.click()`);
      await check('f.posts().length === 0 && f.events.length === 0 && f.navigation.length === 0 && !!f.row()', 'Retired authority never posts or removes a row');
      if (change === 'capability') await check('!f.$("#session-delete-dialog")?.open && document.activeElement === f.row().querySelector("a")', 'Revoked confirmation closes to the surviving link, not its hidden ellipsis');
    });
  }
  await run('pending inventory refresh revokes old popup authority without disabling Rename', {live: '1'}, async () => {
    await e('await f.menu(); window.staleDelete = f.deleteItem(); f.holdInventory("a"); SnowSidebarSessions.invalidate(f.project("a")); staleDelete.dispatchEvent(new MouseEvent("click", {bubbles: true}))');
    await check('f.$("[data-sidebar-project=a] [data-workspace-sessions]").getAttribute("aria-busy") === "true" && !f.$("#session-delete-dialog")?.open && f.posts().length === 0', 'Old enabled menu item cannot authorize deletion while inventory is pending');
    await e('SnowMenus.close(); await f.menu("a", "active")');
    await check('!f.button("a", "active").disabled && ![...f.$(".snow-menu").querySelectorAll("[role=menuitem]")].find(node => node.textContent === "Rename").disabled', 'Inventory uncertainty does not disable the independent current Rename action');
    await e('SnowMenus.close(); f.releaseInventory("a"); await f.idle(); await f.menu()');
    await check('!f.deleteItem().disabled && f.posts().length === 0', 'Fresh inventory restores menu authority without sending a deletion');
  });
  for (const change of ['root', 'project', 'session', 'instance', 'active', 'capability', 'unavailable', 'detached trigger', 'closed popup']) {
    await run(`stale popup ${change} cannot authorize confirmation`, {live: '1'}, async () => {
      await e('await f.menu(); window.staleDelete = f.deleteItem()');
      const mutate = {
        root: 'await f.replaceRoot()',
        project: 'f.button().dataset.project = "b"',
        session: 'f.button().dataset.session = "saved-two"',
        instance: 'f.button().dataset.instance = "replacement-worker"',
        active: 'f.button().dataset.deleteActive = "true"',
        capability: 'f.inventories.a.delete_supported = false; SnowSidebarSessions.invalidate(f.project("a")); await f.idle()',
        unavailable: 'f.button().dataset.deleteAvailable = "false"',
        'detached trigger': 'f.button().remove()',
        'closed popup': 'SnowMenus.close()'
      }[change];
      await e(`${mutate}; staleDelete.dispatchEvent(new MouseEvent("click", {bubbles: true}))`);
      await check('!f.$("#session-delete-dialog")?.open && f.posts().length === 0 && f.events.length === 0 && f.navigation.length === 0', 'Captured popup action cannot authorize a changed or detached owner');
    });
  }
  await run('committed workspace cleanup retires confirmation', {}, async () => {
    await e('await f.open(); await f.replaceRoot()');
    await check('!f.$("#session-delete-dialog")?.open && !!f.button() && f.posts().length === 0', 'Committed navigation tears down unsubmitted destructive confirmation');
  });
  await run('receipt after workspace replacement never navigates the new owner', {viewed: 'saved-one'}, async () => {
    await e('await f.open(); await f.confirm(); await f.replaceRoot("b"); f.succeed(); await f.succeeded()');
    await check('f.navigation.length === 0 && f.$("#workspace").dataset.project === f.project("b") && !!f.row("b") && !f.row()', 'Old receipt removes only original metadata without overwriting superseding workspace');
  });
  for (const width of [1280, 320]) for (const height of [740, 240]) {
    await run(`native focus and responsive geometry ${width}x${height}`, {}, async () => {
      await h.viewport(width, height);
      await e('await f.until(() => SnowShell.snapshot.narrow === (innerWidth < 768), "responsive Shell"); if (innerWidth < 768) { SnowShell.navigation(true); await f.until(() => !f.$("#project-navigation").inert, "mobile navigation"); } f.button().focus()');
      await check('f.visibleHit(f.button()) && f.row().scrollWidth <= f.row().clientWidth + 1', 'Ellipsis remains focus-visible and hit-testable without row overflow');
      await key('Enter');
      await check('document.activeElement === f.deleteItem() && f.visibleHit(f.deleteItem()) && f.posts().length === 0', 'Keyboard-opened popup focuses visible Delete without a request');
      await check('(() => { const menu = f.$(".snow-menu"), r = menu.getBoundingClientRect(); return r.left >= 11 && r.right <= innerWidth - 11 && r.top >= 11 && r.bottom <= innerHeight - 11 && menu.scrollWidth <= menu.clientWidth + 1; })()', 'Shared popup is unclipped at this viewport');
      await key('Enter');
      await e('f.until(() => f.$("#session-delete-dialog")?.open, "keyboard-opened dialog")');
      await check('document.activeElement === f.$("[data-delete-cancel]") && f.visibleHit(document.activeElement)', 'Keyboard opening reaches visible native Cancel');
      await check('(() => { const d = f.$("#session-delete-dialog"), r = d.getBoundingClientRect(); return r.left >= 11 && r.right <= innerWidth - 11 && r.top >= 11 && r.bottom <= innerHeight - 11 && d.scrollWidth <= d.clientWidth + 1; })()', 'Dialog is bounded inside viewport even with a long hostile title');
      await key('Tab');
      await check('document.activeElement === f.$("[data-delete-confirm]") && f.visibleHit(document.activeElement)', 'Tab reaches and scrolls acknowledgement into view');
      await key(' ');
      await check('f.$("[data-delete-confirm]").checked && !f.$("[data-delete-submit]").disabled', 'Native Space explicitly enables destructive submit');
      await key('Tab');
      await check('document.activeElement === f.$("[data-delete-submit]") && f.visibleHit(document.activeElement)', 'Submit is reachable and not clipped at short height');
      await key('Escape');
      await e('f.until(() => !f.$("#session-delete-dialog")?.open, "responsive Escape")');
      await check('document.activeElement === f.button() && f.visibleHit(document.activeElement) && f.posts().length === 0', 'Escape restores a visible trigger without accidental deletion');
    });
  }

  async function assertFailure() {
    await check('f.$("#session-delete-dialog")?.open && !!f.row() && f.events.length === 0 && f.navigation.length === 0', 'Unconfirmed outcome preserves row and emits no success/navigation');
    await check('f.$("[data-delete-submit]").disabled && !f.$("[data-delete-cancel]").disabled && /retry/i.test(f.$("[data-delete-error]").textContent)', 'Failure allows dismissal, not replay');
    await e('f.$("[data-delete-submit]").dispatchEvent(new MouseEvent("click", {bubbles: true})); f.$("[data-delete-confirm]").dispatchEvent(new Event("change", {bubbles: true}))');
    await check('f.posts().length === 1 && f.deadlines.size === 0 && f.$("[data-delete-submit]").disabled', 'Synthetic re-entry cannot retry an uncertain request');
    await e('f.$("[data-delete-cancel]").click()');
    await check('!f.$("#session-delete-dialog")?.open && document.activeElement === f.button() && f.posts().length === 1', 'Failure dismissal restores row focus and does not retry');
  }
}
