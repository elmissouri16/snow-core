# Cat pet

Mochi is a tiny, event-driven ASCII cat that sits just above Snow's composer.
Plain JavaScript using Snow plugin API 2; no npm packages or build step.

```text
 /\_/\
(o.o)  Mochi: keeping you company
 > ^ <  /cat pet | feed | nap | hide
```

## Try it

From the repository root, launch a new Snow TUI with this plugin:

```sh
snow --js-plugin ./examples/plugins/cat-pet
```

This loads it for that launch only, not into an already-running TUI. The cat
appears when the plugin is ready. It takes three lines at normal widths; Snow
may shrink or hide contributions on short terminals. Its view remains accessible
through `/plugins`.

| Command | Effect |
| --- | --- |
| `/cat` or `/cat show` | Show the cat without changing its mood |
| `/cat pet` | Happy face and a purr |
| `/cat feed` | Snack time |
| `/cat nap` | Sleep until you pet, feed, or wake the cat |
| `/cat wake` | Return to the normal face |
| `/cat hide` | Clear the widget; activity will not bring it back |
| `/cat help` | Show command usage |

Each row is a complete command: the widget's `feed`, `nap`, and `hide` hints
are shorthand for `/cat feed`, `/cat nap`, and `/cat hide`.
The namespaced `/cat-pet:cat` command accepts the same arguments if another
plugin already owns the `/cat` alias.

When a turn completes, an awake, visible cat alternates between a wink and its
normal face. There are no timers, polling, raw terminal escapes, or background
processes. Sleeping and hidden cats ignore turn completion. Petting or feeding a
hidden cat changes its mood but does not show it; use `/cat show` explicitly.

State is cosmetic and in memory for the plugin's runtime: it follows you across
session/branch switches in that process, and resets to visible/awake on plugin
reload or Snow restart. No files, storage, conversation history, or model prompts
are changed. The only declared capabilities are `commands` and `ui`; no host
tools, hooks, network calls, or provider requests are used. Snow plugins are
trusted local code, not an OS sandbox.

## Optional persistent registration

Only if you want the cat loaded on future launches:

```sh
snow plugin add ./examples/plugins/cat-pet
snow plugin check cat-pet
snow
```

Registration enables the plugin; restart Snow to load it. Once loaded, edits can
be applied while idle with `/plugins reload cat-pet`. To stop loading it, use
`/plugins disable cat-pet` and restart. `/cat hide` only hides the current widget.

## Verification

Run the network-free Goja fixtures from the repository root:

```sh
snow plugin test ./examples/plugins/cat-pet \
  --fixtures ./examples/plugins/cat-pet/tests/plugin.json --json
```

The six cases cover readiness, default display, input normalization, pet/feed,
nap/wake, event-driven expressions, hide/show, invalid commands, and a failed UI
update without committing the new mood. They assert exact UI host calls as well
as command results. They do not verify real terminal rendering or layout.

Manual checks in a TUI:

1. Launch with the one-launch flag and check that the three-line cat appears
   above the composer without opening a dialog or replacing draft input.
2. Exercise each command and confirm the faces match the descriptions above.
3. Let an ordinary turn finish: the awake cat changes expression once. Repeat
   while sleeping and hidden; neither should be disturbed.
4. Resize to a narrow/short terminal; the composer must remain usable. Access
   the cat view from `/plugins` if Snow hides the contribution.
5. Reload while idle; the cat returns to its initial visible, awake state.

After optional registration, a credential-free headless check is:

```sh
snow plugin run cat-pet:cat -- pet
```

It should return the ASCII happy cat without requesting an interactive UI.
Commands use `ui.update`, which supports headless snapshots; they never open
screens or dialogs. A one-shot headless process does not retain the pet's mood.

These verification commands were not executed during creation because shell
access was denied. The package has been written, not runtime-validated,
registered, or loaded.
