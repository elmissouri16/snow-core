import {useLayoutEffect, useSyncExternalStore} from 'react';
import {createPortal} from 'react-dom';
import type {Navigate, ShellBootstrap, ShellCommand} from './model';
import {shell} from './controller';
import {Sidebar} from './Sidebar';
import {Settings} from './Settings';
import {ShellPopup} from './Menu';
import {DeleteDialog} from './DeleteDialog';
export interface ReactShellProps {
  bootstrap: ShellBootstrap;
  navigationHost?: HTMLElement;
  navigate?: Navigate;
  command?: (value: ShellCommand) => void;
}
export function ReactShell({bootstrap, navigationHost, navigate, command}: ReactShellProps) {
  const view = useSyncExternalStore(shell.subscribe, shell.getSnapshot, shell.getSnapshot);
  // The parent keeps this root stable throughout live updates. A new bootstrap
  // denotes an actual ancestor navigation, not a stream render.
  useLayoutEffect(() => {
    shell.mount(bootstrap, navigate, command);
    return () => shell.dispose();
  }, [bootstrap]);
  useLayoutEffect(() => {
    const narrow = matchMedia('(max-width: 767px)');
    const update = () => { shell.publish({narrow: narrow.matches}); if (!narrow.matches && shell.snapshot.navOpen) shell.navigation(false); };
    update(); narrow.addEventListener('change', update);
    return () => narrow.removeEventListener('change', update);
  }, []);
  useLayoutEffect(() => {
    if (navigate) shell.navigate = navigate;
    if (command) shell.command = command;
  }, [navigate, command]);
  if (!view.bootstrap) return null;
  const sidebar = <Sidebar controller={shell} view={view} />;
  return <>{navigationHost ? createPortal(sidebar, navigationHost) : sidebar}<Settings controller={shell} view={view} />
    {view.menu && <ShellPopup controller={shell} menu={view.menu} />}
    <DeleteDialog controller={shell} />
  </>;
}
