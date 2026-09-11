# Try the extension pack

After registering these packages, restart Snow and launch it normally:

```sh
snow
```

| Case | Try in Snow | What happens |
|---|---|---|
| Sidebar, footer, lifecycle events | `/dashboard` | Shows the workspace, model, and agent status; refreshes after turns. |
| Native UI components and forms | `/ui-studio` | Opens the component gallery. Use Tab to select buttons, Enter to run, arrows to scroll, Escape to close. |
| Themes | `/plugin-theme ocean` or `/plugin-theme amber` | Changes the palette for this run. `/settings` restores a built-in theme. |
| Header and composer contributions | `/ui-studio:form`, then `/ui-studio:show` | Saves a typed project focus and displays it above the conversation/composer. Hide with `/ui-studio:hide`. |
| Persistent state | `/note Check cancellation before merging`, then `/notes` | Saves a project note outside the conversation; it survives restart. |
| Composer editing | `/workspace-notes:insert` or `/recipe review current changes` | Prepares text for you to inspect before sending. |
| Prompt transformations | Send `#explain how sessions are saved` | Expands only the explicit `#review`, `#explain`, or `#test` prefix. |
| Request and tool hooks | `/recipe-mode concise`, then ask to read a file | Adds concise guidance, caps each read to 120 lines, and annotates successful reads. `/recipe-mode off` restores ordinary reads. |
| Commands and shortcuts | Alt+U, Alt+N, `/plugins`, `/keybindings` | Opens UI Studio or Notes; inspects contributions and configurable shortcuts. |
| Models and sessions | `/pilot`, `/pilot-model`, `/pilot-rename My experiment` | Opens controls, selects a configured model, or renames the saved session. |
| Conversation branches | `/pilot-branch fork experiment`, `/pilot-branch switch` | Forks/switches conversation history. These are not Git branches. |
| Agent control | `/pilot-run Explain this project` | Sends a task through the normal agent loop. Use `/pilot-steer text`, `/pilot-followup text`, or `/pilot-stop` during work. |
| Goal control | `/pilot-goal status` or `/pilot-goal create Improve test coverage` | Inspects a goal or confirms starting automatic work. Also supports pause, resume, and clear. |
| Custom tools and result cards | Ask “Use plugin_project-helper-v2_files to find source files” | Runs rooted glob through Snow permissions and shows a custom result card. |
| Selected child plugin tools | `/source-scout Where does the main agent loop live?` | Starts one read-only child with its own selected source-tool runtime. Requires the role below. |
| Multiple reviewers | `/review-team Review my current changes` | Starts three explorer reviewers, displays progress, closes them, and synthesizes their findings. |

Reviewer/scout/agent/goal commands use your configured model and its ordinary
usage. Opening panels, editing notes, and choosing themes do not send a prompt.
Settings are editable through `/plugins`; restart after changing them. Notes
default to project scope; choose session or global scope in their settings.
Concise hooks start **off** each run. Dashboard is the only automatic visible
panel; theme/focus changes are explicit. The project-helper request hook adds
the configured source pattern to agent requests.

The input/select/checkbox nodes in the gallery display values. The **Edit focus**
button uses Snow's native dialog to actually edit those values. On narrow
terminals, open panels with their slash commands instead of relying on a sidebar.

## Register on another installation

Run from this repository using a build with JavaScript API 2 support:

```sh
snow plugin add ./examples/plugins/workspace-dashboard
snow plugin add ./examples/plugins/ui-studio
snow plugin add ./examples/plugins/workspace-notes
snow plugin add ./examples/plugins/prompt-recipes
snow plugin add ./examples/plugins/session-pilot
snow plugin add ./examples/plugins/project-helper-v2
snow plugin add ./examples/plugins/review-team
```

For `/source-scout`, merge this role into your existing global configuration's
`subagents.roles` map, and set `subagents.enabled` to `true`. Preserve other roles
and subagent settings:

```json
{
  "plugin_scout": {
    "description": "Read-only explorer with the project source plugin tool",
    "tools": ["read", "grep", "glob", "plugin_project-helper-v2_files"]
  }
}
```

Registration stores absolute references to these directories. Keep the checkout
in place. No npm installation is needed. To disable one package for future runs:

```sh
snow plugin disable ui-studio
```

`snow --no-plugins` skips every plugin for one run. `snow plugin list` shows your
registrations. `snow plugin check <id>` validates a package without host I/O.

## Verify without a model account

```sh
python3 examples/plugins/extensions_smoke.py
```

The test registers the seven packages in a temporary Snow home and exercises
normal config loading, commands, typed dialogs over RPC, persisted project state,
conversation forks, model control, review cleanup, all four hook phases, actual
file tools, custom cards, and a selected child tool. It uses fake/local providers
and does not modify your registrations. Theme/composer interaction also needs a
manual TUI check. Set `SNOW_BIN=/path/to/snow` to test a particular build.

[API guide](../../docs/plugin-extensions.md) · [Example source index](README.md)
