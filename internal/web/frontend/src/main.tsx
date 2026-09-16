import { Component, useLayoutEffect } from 'react';
import type { ErrorInfo, ReactNode } from 'react';
import { createRoot } from 'react-dom/client';
import { flushSync } from 'react-dom';
import type { Root } from 'react-dom/client';
import { ActivityPage } from './activity/ActivityPage';
import { OrganizationPage } from './organization/OrganizationPage';
import { validateOrganizationProps } from './organization/model';
import { BrowserInventory } from './browser-access/BrowserInventory';
import { HostSettingsPanel } from './host-settings/HostSettingsPanel';
import { reasoning } from './reasoning/index';
import { compaction } from './compaction/index';
import { goals } from './goals/index';
import { versions, historyControls } from './versions/index';
import { conversation } from './conversation/index';
import { steer } from './steer/index';
import queue from './queue/index';
import { composerContext } from './composer-context/index';
import { LiveViewBridge } from './live-view/index';
import { messages, visibility, captureSavedHistory, SavedHistory } from './messages/index';
import type { SavedHistoryProjection } from './messages/index';
import { inspection } from './inspection/index';
import { processes } from './processes/index';
import { attention } from './attention/index';
import { reader } from './reader/index';
import { width } from './width/index';
import { ReactShell, validateShellBootstrap, shell, sidebarSessions, sessionActions } from './shell/index';
import { HomePage, WorkspaceCatalog, ColdWorkspace, LoginPage, DraftNotice, Opening,
  workspace, projectOperations, validateHomeProps, validateCatalogProps, validateColdProps, validateLoginProps } from './workspace/index';
import { workspaceNavigation } from './navigation';

// Only the marked subtree is React-owned. The native workspace navigator may
// replace its ancestor, but never renders inside a mounted root. No
// provider/runtime code lives here.
const roots = new Map<HTMLElement, Root>();
const savedHistories = new WeakMap<HTMLElement, {project: string; session: string; projection: SavedHistoryProjection | null}>();
const maxBootstrapLength = 1024 * 1024;

class PageBoundary extends Component<{children: ReactNode}, {failed: boolean}> {
  state = {failed: false};
  static getDerivedStateFromError() { return {failed: true}; }
  componentDidCatch(_error: Error, _info: ErrorInfo) {
    // Never log server props, credentials or private presentation data.
  }
  render() {
    if (this.state.failed) return <p className="error" role="alert">This page could not be displayed. Reload to try again. No action has been retried.</p>;
    return this.props.children;
  }
}

function MountedPage({element, children}: {element: HTMLElement; children: ReactNode}) {
  useLayoutEffect(() => {
    element.dataset.reactMounted = 'true';
    return () => { delete element.dataset.reactMounted; };
  }, [element]);
  return <PageBoundary>{children}</PageBoundary>;
}

function page(element: HTMLElement): ReactNode {
  const encoded = element.dataset.reactProps || '{}';
  if (encoded.length > maxBootstrapLength) throw new Error('Page data too large');
  const props: unknown = JSON.parse(encoded);
  switch (element.dataset.reactPage) {
    case 'home': return <HomePage {...validateHomeProps(props)} />;
    case 'workspace-catalog': return <WorkspaceCatalog {...validateCatalogProps(props)} />;
    case 'workspace-cold': {
      const cold = validateColdProps(props);
      let saved = savedHistories.get(element);
      if (!saved || saved.project !== cold.project.id || saved.session !== cold.sessionID) {
        saved = {project: cold.project.id, session: cold.sessionID,
          projection: captureSavedHistory(element.querySelector<HTMLElement>('.catalog-history'),
            {project: cold.project.id, session: cold.sessionID})};
        savedHistories.set(element, saved);
      }
      return <ColdWorkspace {...cold} history={<SavedHistory projection={saved.projection} />} />;
    }
    case 'login': return <LoginPage {...validateLoginProps(props)} />;
    case 'startup-draft-notice': return <DraftNotice />;
    case 'workspace-opening': return <Opening />;
    case 'shell': {
      const navigationHost = document.getElementById('shell-navigation-root');
      if (!navigationHost) throw new Error('Missing navigation host');
      return <ReactShell bootstrap={validateShellBootstrap(props)} navigationHost={navigationHost}
        navigate={(href, source) => { document.dispatchEvent(new CustomEvent('snow:shell-navigate', {detail: {href, source}})); }}
        command={command => document.dispatchEvent(new CustomEvent('snow:shell-command', {detail: command}))} />;
    }
    case 'activity': {
      if (!props || typeof props !== 'object' || !('registryEnabled' in props) || typeof props.registryEnabled !== 'boolean' || !('error' in props) || typeof props.error !== 'string' || props.error.length > 4096) throw new Error('Invalid activity page');
      return <ActivityPage registryEnabled={props.registryEnabled} error={props.error} />;
    }
    case 'organization':
      return <OrganizationPage {...validateOrganizationProps(props)} />;
    case 'browser-access': {
      if (!props || typeof props !== 'object' || !('csrf' in props) || typeof props.csrf !== 'string' || props.csrf.length > 512) throw new Error('Invalid browser access page');
      return <BrowserInventory csrf={props.csrf} />;
    }
    case 'host-settings': {
      if (!props || typeof props !== 'object' || !('csrf' in props) || typeof props.csrf !== 'string' || props.csrf.length > 512 ||
          !('enabled' in props) || typeof props.enabled !== 'boolean' ||
          !('projects' in props) || !Array.isArray(props.projects) || props.projects.length > 100) throw new Error('Invalid host settings');
      const ids = new Set<string>();
      const projects = props.projects.map((project: unknown) => {
        if (!project || typeof project !== 'object' || !('id' in project) || typeof project.id !== 'string' ||
            !/^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/.test(project.id) || ids.has(project.id) ||
            !('name' in project) || typeof project.name !== 'string' || new TextEncoder().encode(project.name).length > 128) throw new Error('Invalid project');
        ids.add(project.id);
        return {id: project.id, name: project.name};
      });
      return <HostSettingsPanel csrf={props.csrf} enabled={props.enabled} projects={projects} />;
    }
    default:
      throw new Error('Unsupported page');
  }
}

function mount() {
  for (const [element, root] of roots) {
    if (!element.isConnected) { root.unmount(); roots.delete(element); }
  }
  document.querySelectorAll<HTMLElement>('[data-react-page]').forEach(element => {
    if (roots.has(element) || element.parentElement?.closest('[data-react-page]')) return;
    let content: ReactNode;
    try { content = page(element); }
    catch { content = <p className="error" role="alert">This page could not be loaded safely. Reload to try again. No action has been sent.</p>; }
    const root = createRoot(element, {
      onCaughtError: () => {},
      onUncaughtError: () => {},
      onRecoverableError: () => {},
    });
    roots.set(element, root);
    flushSync(() => root.render(<MountedPage element={element}>{content}</MountedPage>));
  });
}

function cleanup(target: Element) {
  for (const [element, root] of roots) {
    if (target === element || target.contains(element)) { root.unmount(); roots.delete(element); }
  }
}

workspaceNavigation.configure({beforeReplace: cleanup, afterReplace: mount});
window.addEventListener('pageshow', mount);
window.addEventListener('pagehide', () => {
  for (const root of roots.values()) root.unmount();
  roots.clear();
});
if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', mount, {once: true});
else mount();

// Explicit readiness: module loading order alone does not synchronize classic
// controller initialization with the registered React view facades.
Object.assign(window, {SnowReasoning: reasoning, SnowCompaction: compaction, SnowGoals: goals,
  SnowVersions: versions, SnowHistoryControls: historyControls, SnowConversation: conversation,
  SnowSteer: steer, SnowQueue: queue, SnowComposerContext: composerContext,
  SnowLiveView: LiveViewBridge, SnowMessages: messages, SnowVisibility: visibility,
  SnowInspection: inspection, SnowProcesses: processes, SnowAttention: attention, SnowScroll: reader, SnowWidth: width,
  SnowShell: shell, SnowSidebarSessions: sidebarSessions, SnowSessionActions: sessionActions,
  SnowWorkspace: workspace, SnowProjectOperations: projectOperations, SnowNavigation: workspaceNavigation,
  SnowReactReady: true});
document.dispatchEvent(new Event('snow:react-ready'));
