// Focus is delivered by native CDP pointer/Tab events, never target.focus().
export async function runFocusTests({cases, evaluate, send, check, scenario}) {
  const sample = () => evaluate(`(() => {
    const target = document.querySelector('#target'), boundary = document.querySelector('#boundary');
    function metrics(element) {
      if (!element) return null;
      const style = getComputedStyle(element), rect = element.getBoundingClientRect();
      return {outline: [style.outlineWidth, style.outlineStyle, style.outlineOffset],
        color: style.outlineColor, background: style.backgroundColor,
        borderColor: style.borderTopColor, shadow: style.boxShadow,
        size: [rect.width, rect.height], focused: element.matches(':focus'),
        visible: element.matches(':focus-visible'), within: element.matches(':focus-within')};
    }
    const probe = document.createElement('span'); probe.style.color = 'var(--focus)'; document.body.append(probe);
    const focus = getComputedStyle(probe).color; probe.remove();
    return {target: metrics(target), boundary: metrics(boundary), focus};
  })()`);
  async function key(key, code, virtualKey) {
    await send('Input.dispatchKeyEvent', {type: 'keyDown', key, code, windowsVirtualKeyCode: virtualKey});
    await send('Input.dispatchKeyEvent', {type: 'keyUp', key, code, windowsVirtualKeyCode: virtualKey});
  }
  async function pointer(selector) {
    const point = await evaluate(`(() => {
      const element = document.querySelector(${JSON.stringify(selector)}); element.scrollIntoView({block:'center'});
      const rect = element.getBoundingClientRect(); return {x:rect.left + Math.min(8, rect.width / 2), y:rect.top + rect.height / 2};
    })()`);
    await send('Input.dispatchMouseEvent', {type: 'mouseMoved', ...point});
    await send('Input.dispatchMouseEvent', {type: 'mousePressed', ...point, button: 'left', clickCount: 1});
    await send('Input.dispatchMouseEvent', {type: 'mouseReleased', ...point, button: 'left', clickCount: 1});
  }
  function edge(metrics, focus, forced, name) {
    check(metrics.outline, ['1px', 'solid', '-1px'], `${name}: one-pixel inset field edge`);
    if (!forced) check(metrics.color, focus, `${name}: uses theme --focus color`);
    else check(metrics.color !== 'rgba(0, 0, 0, 0)' && metrics.color !== metrics.background, true, `${name}: forced-colors indicator remains visible`);
  }
  for (const forced of [false, true]) for (const theme of ['dark', 'light']) for (const width of [320, 1280]) {
    await send('Emulation.setDeviceMetricsOverride', {width, height: 800, deviceScaleFactor: 1, mobile: false});
    await send('Emulation.setEmulatedMedia', {features: [{name: 'forced-colors', value: forced ? 'active' : 'none'}, {name: 'prefers-color-scheme', value: theme}]});
    await evaluate(`document.documentElement.dataset.theme = ${JSON.stringify(theme)}`);
    check(await evaluate(`matchMedia('(forced-colors: active)').matches`), forced, `${theme}/${width}: native forced-colors emulation`);
    for (const item of cases) {
      scenario();
      const prefix = `${theme}/${width}/${forced ? 'forced' : 'normal'}/${item.name}`;
      await evaluate(`(() => {
        document.querySelector('#fixture').innerHTML = ${JSON.stringify(item.html)};
        const target = document.querySelector('#target'), sentinel = document.createElement('span');
        sentinel.id = 'sentinel'; sentinel.tabIndex = 0;
        sentinel.style.cssText = 'position:fixed;top:0;left:0;width:1px;height:1px;opacity:0';
        target.before(sentinel);
      })()`);
      await pointer('#blur');
      const before = await sample();
      for (const mode of item.kind.endsWith('cue') ? ['keyboard'] : ['pointer', 'keyboard']) {
        if (mode === 'pointer') {
          await pointer('#target');
          // Close a native select/date popover without moving focus.
          await key('Escape', 'Escape', 27);
        } else {
          await evaluate(`document.querySelector('#sentinel').focus()`);
          await key('Tab', 'Tab', 9);
        }
        const focused = await sample(), label = `${prefix}/${mode}`;
        check(focused.target.focused, true, `${label}: native event focuses field`);
        check(focused.target.size, before.target.size, `${label}: field dimensions unchanged`);
        if (focused.boundary) check(focused.boundary.size, before.boundary.size, `${label}: compound dimensions unchanged`);
        if (item.kind === 'field') edge(focused.target, focused.focus, forced, label);
        else if (item.kind === 'composer' || item.kind === 'attention') {
          check(focused.target.outline[1], 'none', `${label}: no inner textarea outline`);
          check(focused.target.shadow, 'none', `${label}: no inner textarea shadow`);
          edge(focused.boundary, focused.focus, forced, label);
          check(focused.boundary.shadow, before.boundary.shadow, `${label}: no additional wrapper halo`);
          if (!forced) check(focused.boundary.borderColor === focused.focus, false, `${label}: no second blue border beside the outline`);
        } else {
          const cue = item.kind === 'wrapper-cue' ? focused.boundary : focused.target;
          check(focused.target.visible, true, `${label}: native keyboard focus-visible matches`);
          check(parseFloat(cue.outline[0]) >= 2 && cue.outline[1] !== 'none', true, `${label}: existing keyboard cue preserved`);
          check(cue.color !== 'rgba(0, 0, 0, 0)', true, `${label}: keyboard cue has visible color`);
        }
        await pointer('#blur');
        const blurred = await sample();
        check(blurred.target.focused, false, `${label}: native pointer blur`);
        check(blurred.target.outline, before.target.outline, `${label}: field focus edge removed on blur`);
        if (blurred.boundary) check(blurred.boundary.outline, before.boundary.outline, `${label}: compound focus edge removed on blur`);
      }
      if (!item.kind.endsWith('cue')) {
        await evaluate(`document.querySelector('#target').disabled = true`);
        await pointer('#target');
        check((await sample()).target.focused, false, `${prefix}: disabled field rejects pointer focus`);
        await evaluate(`document.querySelector('#sentinel').focus()`);
        await key('Tab', 'Tab', 9);
        check((await sample()).target.focused, false, `${prefix}: disabled field skipped by native Tab`);
        await pointer('#blur');
        const disabled = await sample();
        check(disabled.target.outline, before.target.outline, `${prefix}: disabled field has no focus edge`);
        if (disabled.boundary) check(disabled.boundary.outline, before.boundary.outline, `${prefix}: disabled textarea leaves wrapper unfocused`);
      }
    }
  }
}
