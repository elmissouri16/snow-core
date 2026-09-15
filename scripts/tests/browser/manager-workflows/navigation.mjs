// Real manager-only organization and cross-project activity. No fake endpoint.
import {stat} from "node:fs/promises";
import {join} from "node:path";
import {setTimeout as delay} from "node:timers/promises";

export function navigationChecks({ready,directory,click,key,insert,evaluate,wait,check,assert,requests,callCount,snapshot,width}) {
  const q = selector => `document.querySelector(${JSON.stringify(selector)})`;
  const postCount = () => requests.filter(request => request.method === "POST").length;
  const nav = async view => {
    const selector = `a[href="/?view=${view}"]`;
    const onscreen = await evaluate(`(() => {const r=${q(selector)}?.getBoundingClientRect();return r&&r.width>0&&r.left>=0&&r.right<=innerWidth&&getComputedStyle(${q(selector)}).visibility==='visible';})()`);
    if (!onscreen) await click("[data-nav-toggle]");
    await click(selector);
    await wait(`${q('#workspace')}?.dataset.view===${JSON.stringify(view)}`, `native ${view} navigation`);
  };
  const form = action => `form[action="/projects/${ready.project}/organization/${action}"]`;
  const submit = async action => {
    // Organization uses native POST/redirect documents, not an HTMX swap.
    // React can expose the next form before load/scroll restoration finishes;
    // do not aim the next pointer action from that intermediate layout.
    await evaluate('window.managerOrganizationSubmitting = true');
    await click(form(action) + ' button[type="submit"]');
    await wait('window.managerOrganizationSubmitting !== true && document.readyState === "complete" && window.SnowReactReady === true', `native ${action} redirect document is ready`);
    await evaluate('new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve(true))))');
  };
  const projectLink = `a[href="/?view=organization&project=${ready.project}"]`;
  const scopeProject = async () => { if (!await evaluate(`!!${q('[data-organization-sessions]')}`)) await click(projectLink); await wait(`!!${q('[data-organization-sessions]')}`, "organization scopes exact registration"); };
  const backToProject = async () => {
    const liveLink = `[data-organization-sessions] a[href="/?view=projects&project=${ready.project}"]`;
    const link = await evaluate(`!!${q(liveLink)}`) ? liveLink : `a[href="/?view=projects&project=${ready.project}"]`;
    if (!await evaluate(`(() => {const r=${q(link)}?.getBoundingClientRect();return r&&r.width>0&&r.left>=0&&getComputedStyle(${q(link)}).visibility==='visible';})()`)) await click("[data-nav-toggle]");
    await click(link); await wait(`!!${q('[data-runtime-open]')} || ${q('#live-connection')}?.textContent==='Live'`, "native return to exact registered project");
  };
  const bounds = label => assert('document.documentElement.scrollWidth<=innerWidth+1', `${label}: no horizontal overflow at ${width}px`);
  const activity = async (expected, sessionID) => {
    const beforePosts = postCount(), beforeCalls = await callCount();
    await nav("activity");
    const card = `.manager-activity-card[data-project="${ready.project}"]`;
    await wait(`${q('[data-manager-activity]')}?.dataset.freshness==='fresh' && !!${q(card)}`, "actual activity summary becomes fresh");
    await assert(`${q(card)}.textContent.includes(${JSON.stringify(expected)})`, `Real Activity shows ${expected} from actual runtime metadata`);
    const summary = await evaluate(`fetch('/activity',{cache:'no-store'}).then(r=>r.json())`);
    const projected = summary.projects.find(project => project.project_id === ready.project);
    check(summary.projects.length === 1 && projected?.session_id === sessionID, "Activity lists exact isolated registered project and active session, not another worker");
    check(!/queue_token|cancel_token|edit_token|restore_token|\"messages\"|\"text\"|\"permission\"\s*:/.test(JSON.stringify(summary)), "Activity summary omits transcript text and mutation authority");
    const href = await evaluate(`${q(card + ' a')}.getAttribute('href')`), target = new URL(href, ready.origin);
    check(target.origin === ready.origin && target.searchParams.get("project") === ready.project && target.searchParams.get("session") === sessionID, "Activity generates exact same-origin project/session navigation");
    await assert(`${q('[data-manager-activity]')}.textContent.includes('Host-running is not browser-connected')`, "Activity distinguishes host execution from browser freshness");
    await click("[data-manager-activity-refresh]"); await delay(120);
    await bounds("Real Activity");
    check(postCount() === beforePosts && await callCount() === beforeCalls, "Activity navigation and explicit refresh only read; no activation, prompt or replay");
    await click(card + " a"); await wait(`${q('#live-connection')}?.textContent==='Live'`, "Activity link opens exact real conversation");
    check((await snapshot()).session_id === sessionID && postCount() === beforePosts && await callCount() === beforeCalls, "Opening Activity conversation rejoins existing session without mutation or provider work");
  };
  return {
    async organizeInactive() {
      const identity = await stat(join(directory, "project")), beforePosts = postCount();
      await nav("organization"); await bounds("Real organization");
      await assert(`${q('.organization-panel')}.textContent.includes('Nothing here starts an agent')`, "Organization explicitly scopes labels, pins and archives to manager metadata");
      check(postCount() === beforePosts && await callCount() === 0, "Opening Organization neither mutates nor starts an agent");
      await click(form("rename") + ' input[name="name"]'); await key("a","KeyA",65,process.platform === "darwin" ? 4 : 2); await insert("Browser workflow workspace"); await submit("rename");
      await wait(`${q(form("rename") + ' input[name="name"]')}?.value==='Browser workflow workspace' && ${q('.organization-item-heading')}?.textContent.includes('Browser workflow workspace')`, "actual registration label save");
      check(requests.filter(request => request.method === "POST" && request.path.endsWith("/organization/rename")).length === 1, "Native label Save submits exactly one real manager rename");
      await submit("pin"); await wait(`!!${q(form("unpin"))}`, "real workspace pin persisted");
      await assert(`${q(form('unpin'))}.closest('.organization-item').textContent.includes('Pinned')`, "Real registration pin badge follows persisted manager state");
      await submit("unpin"); await wait(`!!${q(form("pin"))}`, "real workspace unpin persisted");
      const confirmation = `details:has(${form("archive")})`;
      await click(confirmation + ' > summary');
      await wait(`${q(confirmation)}?.open === true`, "native Archive disclosure opens");
      await submit("archive");
      await wait(`!!${q(form("restore"))} && !${q(form("rename"))}`, "explicit archive moves exact registration out of active workspaces");
      await assert(`${q(form("restore"))}.closest('.organization-item').textContent.includes('Archived')`, "Archived registration remains visibly reviewable and restorable");
      await submit("restore"); await wait(`!!${q(form("rename"))} && !${q(form("restore"))}`, "explicit restore recovers same registration ID");
      const restored = await stat(join(directory, "project"));
      check(identity.ino === restored.ino && identity.dev === restored.dev, "Real archive/restore leaves original project directory identity intact");
      check(postCount() === beforePosts + 5 && await callCount() === 0, "Rename/pin/unpin/archive/restore are exactly five explicit manager mutations, zero provider work");
      await backToProject(); await wait(`!!${q('[data-runtime-open]')}`, "restored project remains inactive until explicit activation");
    },
    activity,
    async organizeLive(sessionID) {
      const beforePosts = postCount(), beforeCalls = await callCount();
      await nav("organization"); await scopeProject();
      await assert(`${q('[data-organization-sessions]')}.textContent.includes('close it before organizing saved conversations')`, "Real organization refuses to hide or reorganize a live conversation");
      await assert(`!${q('[data-organization-session]')} && !${q('form[action*="/sessions/organization/"]')}`, "Live session catalog exposes no saved-session archive/pin mutation controls");
      await bounds("Live organization guard");
      check(postCount() === beforePosts && await callCount() === beforeCalls, "Live organization navigation never closes, switches or starts workers");
      await backToProject(); check((await snapshot()).session_id === sessionID, "Returning from organization preserves exact live session");
    },
    nav,
    backToProject
  };
}
