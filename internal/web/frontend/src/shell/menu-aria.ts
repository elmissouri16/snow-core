import type {ShellMenu} from './controller';

// One shell popup can exist at a time. The host keeps this ID for its lifetime;
// React launchers refer to it only while their own canonical popup is open.
export const SHELL_MENU_ID = 'snow-shell-menu';
export function menuLauncherARIA(menu: Pick<ShellMenu, 'kind' | 'project' | 'session'> | null,
  kind: ShellMenu['kind'], project = '', session = '', available = true) {
  const expanded = available && menu?.kind === kind && menu.project === project && menu.session === session;
  return {'aria-expanded': !!expanded, 'aria-controls': expanded ? SHELL_MENU_ID : undefined};
}
