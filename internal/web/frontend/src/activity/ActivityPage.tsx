import {useRef} from "react";
import {countLabels, projectLink, stateLabels} from "./summary.ts";
import type {ActivityCounts, ActivityProject} from "./summary.ts";
import {useActivity} from "./useActivity.ts";

export interface ActivityPageProps {
  registryEnabled: boolean;
  error?: string;
}

function ProjectCounts({counts}: {counts: ActivityCounts}) {
  return (Object.keys(countLabels) as (keyof ActivityCounts)[]).map(key => (
    <dl className="manager-activity-count" key={key}>
      <dt>{countLabels[key]}</dt><dd>{counts[key]}</dd>
    </dl>
  ));
}

function ProjectCard({project}: {project: ActivityProject}) {
  const details: string[] = [];
  if (project.permissions) details.push("Approval needed. Review it in the conversation.");
  if (project.questions) details.push("A question request needs your answer in the conversation.");
  if (project.failed) details.push("Failure observed. Review before explicitly retrying.");
  if (project.recovery) details.push("Recovery review needed; completion or admission may be uncertain. Nothing is replayed.");
  if (project.queued) details.push(`${project.queued} queued follow-up${project.queued === 1 ? "" : "s"}.`);
  if (project.review) details.push(`${project.review} retained queue item${project.review === 1 ? "" : "s"} to review. No automatic resend.`);
  if (project.folder_state !== "available") details.push("Host folder is unavailable or changed. Review the registration.");
  if (project.unavailable) details.push("Some summary metadata is unavailable. Open the project to review.");
  if (!details.length) details.push(project.session_id ? "Open the saved conversation to review its current state." : "No attention requested. Opening this project does not activate an agent.");
  const attention = !!(project.permissions || project.questions || project.failed || project.recovery || project.review || project.unavailable);
  return (
    <article className="manager-activity-card" data-project={project.project_id} data-attention={String(attention)}>
      <h3>{project.name || "Registered project"}</h3>
      <p className="manager-activity-state">{stateLabels[project.runtime_state]}</p>
      <p className="manager-activity-details">{details.join(" ")}</p>
      <a className="button manager-activity-link" href={projectLink(project.project_id, project.session_id)}
        aria-label={`${project.session_id ? "Open conversation" : "Open project"} in ${project.name || "registered project"}`}>
        {project.session_id ? "Open this conversation →" : "Open project →"}
      </a>
      <p className="manager-activity-session mono">{project.session_id ? "Session " + project.session_id : ""}</p>
    </article>
  );
}

/** Navigation-only Activity island; no runtime subscriptions, commands, or persistence. */
export function ActivityPage({registryEnabled, error}: ActivityPageProps) {
  const root = useRef<HTMLElement>(null);
  const view = useActivity(root, registryEnabled);
  return (
    <section ref={root} className="manager-activity" data-manager-activity="" data-freshness={view.freshness} aria-labelledby="manager-activity-title">
      <header className="workspace-heading">
        <div><span className="eyebrow">ACROSS YOUR PROJECTS</span><h1 id="manager-activity-title">Activity &amp; attention</h1></div>
        <span className="pill">Read-only</span>
      </header>
      <p className="manager-activity-intro">See what is running on this host and what needs your attention. Open the exact conversation to review an approval, answer a question, stop work, or inspect retained queue items.</p>
      <div className="manager-activity-connection">
        <p data-manager-activity-fresh="" role="status">{view.message}</p>
        <button type="button" className="quiet" data-manager-activity-refresh="" onClick={view.refresh}
          disabled={!registryEnabled || view.busy || view.accessEnded || view.freshness === "paused"}>Refresh summary</button>
      </div>
      <p className="fine" data-manager-activity-time="">{view.summary ? "Host snapshot: " + new Date(view.summary.updated_at).toLocaleTimeString() + ". Counts can overlap; questions count pending requests, not individual prompts." : ""}</p>
      <p className="manager-activity-note fine">Host-running is not browser-connected. Leaving this page or losing the browser connection does not stop admitted work. Refreshing only reads current state; it never activates workers or replays work.</p>
      {error && <p className="error" role="alert">{error}</p>}
      {!registryEnabled && <p className="error">The project registry is unavailable. Activity cannot be loaded.</p>}
      <div className="manager-activity-counts" data-manager-activity-counts="" aria-label="Project activity counts">
        {view.summary && <ProjectCounts counts={view.summary.counts} />}
      </div>
      <h2 className="manager-activity-projects-title">Registered projects</h2>
      <p data-manager-activity-empty="" hidden={!view.summary || view.summary.projects.length !== 0}>
        No projects registered yet. <a className="text-link" href="/?view=projects">Choose a host folder in Projects</a> to get started. No agent starts until you explicitly activate it.
      </p>
      <div className="manager-activity-cards" data-manager-activity-cards="">
        {view.summary?.projects.map(project => <ProjectCard key={project.project_id} project={project} />)}
      </div>
      <a className="button" href="/login" data-manager-activity-login="" hidden={!view.accessEnded}>Pair this browser again</a>
      <noscript><p>Activity summaries require JavaScript. You can still <a className="text-link" href="/?view=projects">open Projects</a> and review individual conversations. No work has been started.</p></noscript>
    </section>
  );
}
