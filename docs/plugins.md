# Go Plugins

Plugins extend an embedded Snow session with tools and event handlers. Implement
`pkg/plugin.Plugin` in Go and pass it to `snowsdk.Options.GoPlugins`.

Snow supports in-process Go plugins only. The external executable host,
JavaScript/Python examples, `snow plugin` commands, and `--plugin` flag have been
removed. Existing `plugins` keys in global or project configuration are ignored.

## Choose an extension

| Need | Use |
|---|---|
| Reusable instructions and resources | [Agent Skills](skills.md) |
| External tools, resources, or prompts | [MCP](mcp.md) |
| Go tools and event handlers inside your application | Go plugins |

## Implement a plugin

The `plugin.Plugin` interface has three methods:

```go
type Plugin interface {
    Manifest() Manifest
    Register(context.Context, Registrar) error
    Close(context.Context) error
}
```

- `Manifest` supplies an ID, name, and version. IDs use lowercase letters,
  digits, underscores, and hyphens, with a maximum of 64 characters.
- `Register` adds tools with `Registrar.RegisterTool` and observes agent events
  with `Registrar.Subscribe`.
- `Close` releases resources owned by the plugin.

A tool definition includes its name, description, JSON parameters schema, risk,
and executor. Snow namespaces tool names as `plugin_<id>_<name>`. Risk is `read`,
`write`, `exec`, or `network`; omission defaults to `exec`. Tool calls pass
through Snow's permission gate and receive the session ID, working directory,
call ID, and a progress callback. Results and progress are bounded.

Event handlers receive sanitized copies of agent events. They observe events;
they cannot modify or veto them. Keep handlers short because they run inline.

See the [public Go contract](https://github.com/elmissouri16/snow-core/blob/main/pkg/plugin/plugin.go)
for the complete types.

## Register with the SDK

Pass your implementation when opening a session:

```go
session, err := snowsdk.Open(ctx, snowsdk.Options{
    NoSession: true,
    GoPlugins: []plugin.Plugin{myPlugin},
})
if err != nil {
    return err
}
defer session.Close()
```

Snow registers plugins before the agent starts. Invalid or duplicate
registrations fail startup; a failed registration is rolled back. Closing the
session unregisters tools and subscriptions and closes plugins in reverse load
order. Set `NoPlugins: true` to skip supplied Go plugins.

Plugins are compiled into the embedding application. Snow does not load Go
shared objects or discover plugins from configuration files. See the
[Go SDK guide](sdk.md) for session setup and lifecycle.

## Permissions

Go plugins run inside the host process with its OS privileges. Declared tool
risks control Snow's permission checks; they do not contain plugin code or its
lifecycle methods. Only include code you trust. See the [security model](security.md).
