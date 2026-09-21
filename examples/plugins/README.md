# JavaScript plugins to try

These local plugins work with the Goja-enabled Snow build on this branch.
For TUI customization, hooks, storage, and agent controls, start with the
[API 2 test pack](TRY.md).

From the repository root, load all three using your configured provider:

```sh
./examples/plugins/try.sh
```

The launcher uses your current directory as the project and loads the packages
for this launch. It does not change your plugin registrations. You can pass
normal Snow flags, such as `--provider opencode-go`. Set `SNOW_BIN` to use a
specific Snow executable.

| Plugin | Tools | Try asking Snow |
|---|---|---|
| [Project Context](project-context/main.js) | `plugin_project-context_brief` | “Use the project-context plugin to give me a project overview.” |
| [TODO Radar](todo-radar/main.js) | `plugin_todo-radar_scan` | “Use todo-radar to find up to 20 FIXME markers in Go files.” |
| [Git Review](git-review/main.js) | `plugin_git-review_status`, `plugin_git-review_diff` | “Use git-review to show status and summarize my unstaged diff.” |

## Project Context

Collects a sample of up to 40 files plus previews of `README.md`, `go.mod`,
`package.json`, `pyproject.toml`, and `Cargo.toml`. Each document is limited to
60 lines and 3,500 JavaScript characters. Missing or unreadable optional files
are reported explicitly. The sample is not a complete inventory.

Arguments: `{"path":"."}`. The optional path must stay relative to the project
without `..` components. Uses rooted `glob` and `read`; works in Plan mode.

```sh
snow --js-plugin ./examples/plugins/project-context --permission deny \
  -p 'Use plugin_project-context_brief to inspect this project.'
```

## TODO Radar

Searches for whole-word TODO/FIXME/HACK/XXX markers, case-insensitively, with
file and line references. Matches can include strings or documentation; they
are candidates to review, not a parsed comment audit. Ignore rules apply, and
common dependency/build directories and lockfiles are excluded.

Arguments: `{"path":".","glob":"**/*.go","kind":"fixme","limit":20}`.
All are optional; kind defaults to `all`, limit to 60, and the hard maximum is
200. Uses rooted `grep`; works in Plan mode.

Optional configuration in an existing `js_plugins` entry:

```json
{
  "js_plugins": {
    "todo-radar": {
      "path": "/absolute/path/to/examples/plugins/todo-radar",
      "config": {"default_limit": 40, "exclude": ["**/fixtures/**"]}
    }
  }
}
```

`exclude` adds up to 20 glob patterns to the defaults. Configuration is checked
at startup; invalid limits and patterns with invalid types fail initialization.

## Git Review

Requires Git on PATH. `status({})` shows the branch, tracked changes, and
untracked file names. `diff({"scope":"unstaged","stat":false})` returns a
patch; use `scope: "staged"` for the index, or `stat: true` for a compact summary.
Diffs cover tracked files under the current directory, not untracked contents.

Commands are fixed: user strings are never inserted into a shell command.
Optional index refreshes, pagers, external diff, textconv, and submodule
inspection are disabled. Git's normal configuration and attributes still
apply, including any configured fsmonitor. The plugin does not pass inline
`git -c` overrides, which Snow's shell preflight rejects. Output and execution
time remain bounded by Snow.

This plugin uses `bash`, so Snow classifies it as **exec**. Use it interactively
and approve the requested commands. Both the outer plugin and nested Bash
operation pass permission checks. Plan mode and headless `--permission deny`
block it. The plugin does not stage, commit, fetch, or run project tests.

## Register or test individually

```sh
snow plugin add ./examples/plugins/project-context
snow plugin add ./examples/plugins/todo-radar
snow plugin add ./examples/plugins/git-review
snow plugin check project-context
snow plugin check todo-radar
snow plugin check git-review
snow plugin list
```

`add` enables a directory by reference for future launches; `check` initializes
the script without running its tools. `disable`/`enable` toggle a registration;
`remove` keeps package files. Restart Snow after editing a plugin.

Test your installed binary without a model API or credentials:

```sh
python3 examples/plugins/smoke.py
```

This runs all three plugins through the launcher against a local mock provider
and a temporary Git project. Command permission is enabled only for that
temporary run; your Snow configuration is not changed. Git and Python 3 are
required. Use `SNOW_BIN=/path/to/snow` to select a different build.

Run the deeper SDK integration tests:

```sh
go test ./pkg/snowsdk -run 'TestJavaScript(ProjectContext|TodoRadar|GitReview)' -count=1
```

Tests load these exact packages through the SDK and normal agent loop, using
real rooted file/search tools and a temporary Git repository. They verify
filters, limits, staged/unstaged separation, permission denials, Plan mode, and
one transcript result per outer plugin call. Git tests skip if Git is missing.

## Minimal authoring examples

[Text Tools](text-tools/main.js) exposes `plugin_text-tools_word_count` without
host access. [Project Helper](project-helper/main.js) exposes
`plugin_project-helper_read_file` and logs a `turn_done` observation.

No npm install, Node.js, or Go compiler is needed to run these plugins. The
adjacent [snow.d.ts](snow.d.ts) provides API 1 editor types. Runtime module loading
and API 1 async handlers are unsupported. See [the plugin guide](../../docs/plugins.md)
for the full contract, configuration, and permissions.

## API 2 UI and workflow examples

- `workspace-dashboard`: footer status, sidebar, and `/dashboard` screen.
- `review-team`: `/review-team` starts three explorer reviewers and synthesizes
  their results through the root prompt. Enable subagents; normal provider usage
  applies. Cancellation cleans up only this command's children.
- `project-helper-v2`: typed settings, prompt-composer command, pure request hook,
  a custom tool card, and an explicitly child-selectable source-search tool.
- `ui-studio`: component gallery, typed focus form, header/composer contributions,
  Ocean/Amber themes, notifications, and Alt+U shortcut.
- `workspace-notes`: persistent project/session/global notes, native dialogs,
  composer insertion, and Alt+N shortcut.
- `prompt-recipes`: explicit prompt prefixes and opt-in concise request/read hooks.
- `session-pilot`: model selection, conversation branches, root prompt/guidance/
  cancellation, and explicit goal controls.

[Try each case](TRY.md), including the restricted `plugin_scout` role needed by
`/source-scout`. Run `python3 examples/plugins/extensions_smoke.py` for the
offline API 2 test pack.

Use `snow --js-plugin ./examples/plugins/<directory>` for one launch or
`snow plugin add ./examples/plugins/<directory>` for normal startup loading.
`snow-v2.d.ts` supplies editor types. See
[JavaScript extensions](../../docs/plugin-extensions.md) for the complete API.
