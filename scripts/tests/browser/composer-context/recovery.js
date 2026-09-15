window.testComposerRecovery = async assert => {
  "use strict";
  const {$, tick, wait, posts, latest, draft, chips, clear, upload, textFile, reading, rows, files, skills, swap, key, update} = contextTest;
  await swap({session_id: "session-one", instance_id: "instance-recovery"});
  await clear(); upload([textFile("recover.txt", "retained full file")]); await wait(() => chips().length === 1 && !reading()); draft("keep this draft");
  window.SnowMenus.close(); $("[data-session-menu]").click();
  $("[data-menu-key=sessions]").click();
  // Session inventory is read from the public host choices only when needed.
  if (!$("[data-menu-key=session-two]")) {
    const load = $("[data-menu-key=load]"); if (load && !load.disabled) load.click();
    await wait(() => !!latest("choices") && !latest("choices").settled, "session choices");
    latest("choices").resolve({...fixture.choices, instance_id: "instance-recovery"});
    await wait(() => !!$("[data-menu-key=session-two]"));
  }
  $("[data-menu-key=session-two]").click(); await wait(() => !!latest("switch"));
  assert(latest("switch").fields.instance_id === "instance-recovery" && latest("switch").fields.session_id === "session-two", "switch with context keeps old immutable runtime binding");
  latest("switch").reject(); await wait(() => !$("[data-runtime-reviewed]").disabled, "failed-switch review");
  assert(chips().length === 1 && !$(".composer-context-chip.is-error") && $("#live-prompt").value === "keep this draft", "failed switch retains completed attachment and exact original text");
  $("[data-runtime-reviewed]").click(); await wait(() => !$("#live-send").disabled);
  assert($("#live-session").dataset.instance === "instance-recovery" && posts("switch").length === 1, "explicit outcome review recovers original instance without replaying switch");
  const fileCalls = posts("files").length;
  draft("@"); await wait(() => posts("files").length === fileCalls + 1, "file discovery after failed switch");
  latest("files").resolve(files()); await wait(() => rows().length === 3);
  assert(rows().some(row => row.textContent.includes("notes.txt")), "rebound file callback works after failed-switch controller rotation");
  key("Escape"); const skillCalls = posts("skills").length;
  draft("$"); await wait(() => posts("skills").length === skillCalls + 1, "skill discovery after failed switch");
  latest("skills").resolve(skills()); await wait(() => rows().length === 3);
  assert(rows().some(row => row.textContent.includes("$review")) && chips().length === 1, "rebound skill callback works while retained attachment stays owned");
  key("Escape");
  update({queue: {token: "retained-queue", revision: 1, can_enqueue: false, items: [{id: "held-item", text: "Held follow-up", state: "held"}]}}); await tick(150);
  assert(chips().length === 1 && !chips()[0].disabled, "retained queue review does not deadlock local attachment removal");
  const mutations = posts().length; chips()[0].click(); await tick();
  assert(chips().length === 0 && posts().length === mutations, "removing retained context remains local while queue admission is blocked");
  update({queue: null}); await clear(); await tick();
};
