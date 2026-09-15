// Isolated native controls with production component ancestry. Layout-only inline
// styles keep unrelated app positioning out of this focused cascade regression.
// No fixture stylesheet, focus rule, application script, manager or runtime.
export const cases = [
  ...['text', 'search', 'password', 'email', 'url', 'number', 'date', 'tel'].map(type => ({
    name: `input-${type}`, html: `<input id="target" type="${type}" aria-label="${type}">`, kind: 'field',
  })),
  {name: 'implicit-text', html: '<input id="target" aria-label="Default text type">', kind: 'field'},
  {name: 'select', html: '<select id="target" aria-label="Select"><option>Default</option><option>Other</option></select>', kind: 'field'},
  {name: 'textarea', html: '<textarea id="target" aria-label="Text area">Draft</textarea>', kind: 'field'},
  {name: 'sidebar-search', html: '<div class="sidebar-search"><input id="target" type="search" data-sidebar-search placeholder="Search workspaces…" aria-label="Search workspaces"></div>', kind: 'field'},
  {name: 'model-search', html: '<div class="model-search-header"><input id="target" class="model-search" type="search" aria-label="Search models"></div>', kind: 'field'},
  {name: 'organization-rename', html: '<section class="organization-panel"><form class="organization-rename"><label for="target">Workspace label</label><input id="target" name="name" value="Workspace"></form></section>', kind: 'field'},
  {name: 'organization-search', html: '<section class="organization-panel"><div class="organization-filters"><label>Search titles<input id="target" type="search" data-organization-filter></label></div></section>', kind: 'field'},
  {name: 'reasoning-select', html: '<dialog open style="position:static;margin:0;width:100%" class="reasoning-dialog runtime-dialog"><div class="runtime-dialog-body"><label for="target">Preference</label><select id="target" data-reasoning-field><option>Thinking</option></select></div></dialog>', kind: 'field'},
  {name: 'runtime-text', html: '<dialog open style="position:static;margin:0;width:100%" class="runtime-dialog"><div class="runtime-dialog-body"><label for="target">History name</label><input id="target" type="text" data-history-name></div></dialog>', kind: 'field'},
  {name: 'runtime-textarea', html: '<dialog open style="position:static;margin:0;width:100%" class="runtime-dialog"><div class="runtime-dialog-body"><textarea id="target" data-steer-text rows="6" aria-label="Steer draft"></textarea></div></dialog>', kind: 'field'},
  {name: 'project-inspector', html: '<aside class="project-inspector" style="position:static;width:100%;height:auto"><label for="target">Project name</label><input id="target" name="name" value="Workspace"></aside>', kind: 'field'},
  {name: 'login', html: '<main class="login-card" style="width:100%"><label for="target">Pairing code</label><input id="target" name="code" type="password" autocomplete="off"></main>', kind: 'field'},
  {name: 'cold-draft', html: '<form class="workspace-session-start activation-panel"><textarea id="target" class="activation-draft" rows="2" data-draft-project="fixture" aria-label="Workspace draft"></textarea></form>', kind: 'field'},
  {name: 'host-settings-select', html: '<section class="settings-group host-settings"><div class="host-settings-target"><label>Defaults scope<select id="target" data-host-scope><option>Global defaults</option></select></label></div></section>', kind: 'field'},
  {name: 'host-api-key', html: '<section class="settings-group host-api-key"><label>New API key<input id="target" type="password" data-api-key-secret autocomplete="off"></label></section>', kind: 'field'},
  {name: 'home-composer', html: '<form id="boundary" class="composer home-composer"><textarea id="target" aria-label="Prompt"></textarea><div class="composer-actions"><button type="button" class="home-send">Start</button></div></form>', kind: 'composer'},
  {name: 'stacked-composer', html: '<div class="composer-stack"><form id="boundary" class="composer"><textarea id="target" aria-label="Prompt"></textarea><div class="composer-actions"><button type="button" class="composer-send" aria-label="Send">↑</button></div></form></div>', kind: 'composer'},
  {name: 'attention-custom', html: '<section class="attention-card"><div id="boundary" class="attention-custom attention-custom-block"><div class="attention-answer-field"><span class="attention-answer-mirror" aria-hidden="true">Draft</span><textarea id="target" class="attention-answer" aria-label="Custom answer">Draft</textarea></div></div></section>', kind: 'attention'},
  {name: 'attention-inline-custom', html: '<section class="attention-card"><div id="boundary" class="attention-custom"><input class="attention-radio" type="radio" name="custom" tabindex="-1"><label class="attention-custom-label" for="target"><span class="attention-number">2</span></label><div class="attention-answer-field"><span class="attention-answer-mirror" aria-hidden="true">Draft</span><textarea id="target" class="attention-answer" aria-label="Custom answer">Draft</textarea></div></div></section>', kind: 'attention'},
  {name: 'button', html: '<button id="target" type="button">Action</button>', kind: 'cue'},
  {name: 'link', html: '<a id="target" href="#fixture">Link</a>', kind: 'cue'},
  {name: 'checkbox', html: '<label><input id="target" type="checkbox"> Consent</label>', kind: 'cue'},
  {name: 'radio', html: '<label><input id="target" type="radio" name="choice"> Choice</label>', kind: 'cue'},
  {name: 'organization-checkbox', html: '<section class="organization-panel"><label class="organization-checkbox"><input id="target" type="checkbox"> Show archived</label></section>', kind: 'cue'},
  {name: 'organization-radio', html: '<section class="organization-panel"><label><input id="target" type="radio"> Choice</label></section>', kind: 'cue'},
  {name: 'organization-button', html: '<section class="organization-panel"><button id="target" type="button">Save label</button></section>', kind: 'cue'},
  {name: 'organization-link', html: '<section class="organization-panel"><a id="target" href="#fixture">Back to workspaces</a></section>', kind: 'cue'},
  {name: 'composer-button', html: '<div class="composer-stack"><form class="composer"><div class="composer-actions"><button id="target" type="button" class="composer-send" aria-label="Send">↑</button></div></form></div>', kind: 'cue'},
  {name: 'attention-radio', html: '<section class="attention-card"><label id="boundary" class="attention-option"><input id="target" class="attention-radio" type="radio" name="attention"><span class="attention-number">1</span><span class="attention-option-copy">Choice</span></label></section>', kind: 'wrapper-cue'},
];

export function fixtureHTML(css) {
  return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">${css.map(path => `<link rel="stylesheet" href="${path}">`).join('')}</head><body><main id="fixture" style="padding:24px 12px;max-width:800px;margin:0 auto"></main><button id="blur" type="button" style="position:fixed;bottom:8px;left:8px">Outside field</button></body></html>`;
}
