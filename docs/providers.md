# Choose and configure a provider

Snow supports OpenCode Zen, OpenCode Go, ChatGPT, and OpenAI-compatible
endpoints. Choose one provider, complete its setup, then launch Snow from the
project you want to work on.

> **Warning:** Review a provider's privacy and training policy before sending
> private code. Keep API keys and OAuth tokens out of `config.json`.

## On this page

- [Compare providers](#compare-providers)
- [OpenCode Zen](#opencode-zen)
- [OpenCode Go](#opencode-go)
- [ChatGPT](#chatgpt)
- [OpenAI-compatible endpoints](#openai-compatible-endpoints)
- [Common commands](#common-commands)
- [Troubleshooting](#troubleshooting)
- [Related documents](#related-documents)

## Compare providers

| Provider | Authentication | Start Snow |
|---|---|---|
| OpenCode Zen | None required; OpenCode key optional | `snow --provider opencode-zen` |
| OpenCode Go | OpenCode API key | `snow --provider opencode-go` |
| ChatGPT | Browser or device-code OAuth | `snow --provider chatgpt` |
| OpenAI-compatible | Endpoint and optional Bearer key | `snow --provider NAME` |

A fresh configuration defaults to `opencode-zen`. Add `--model MODEL` to a
launch command when you need to select a specific available model.

## OpenCode Zen

OpenCode Zen works without a credential:

```sh
snow --provider opencode-zen
```

Its promotional free-model availability and quotas can change. To use an
optional OpenCode API key, store it before launching Snow:

```sh
snow login opencode-zen
snow --provider opencode-zen
```

The `OPENCODE_API_KEY` environment variable and `--api-key` flag are also
accepted for the current process.

## OpenCode Go

Store an OpenCode API key, then start Snow:

```sh
snow login opencode-go
snow auth check opencode-go
snow --provider opencode-go
```

For a temporary shell session, use the environment variable instead:

```sh
export OPENCODE_API_KEY=oc-...
snow --provider opencode-go
```

## ChatGPT

Sign in with ChatGPT/Codex subscription OAuth, verify the login, then start
Snow:

```sh
snow login chatgpt
snow auth check chatgpt
snow --provider chatgpt
```

If browser login is unavailable, use the device-code flow:

```sh
snow login chatgpt --device-code
```

This login is separate from OpenAI API-key authentication. Run
`snow logout chatgpt` to remove the credential stored by Snow. See the detailed
[ChatGPT authentication reference][chatgpt-auth] for OAuth troubleshooting.

## OpenAI-compatible endpoints

Create a named profile for a persistent endpoint that uses a Bearer key. Snow
prompts for the key and saves the endpoint in `~/.snow/config.json`:

```sh
snow login openai-compatible \
  --name my-provider \
  --base-url https://gateway.example/v1

snow --provider my-provider
```

The profile name becomes the value passed to `--provider`. Named profiles keep
endpoints and credentials separate; a profile can also describe a keyless
endpoint when added directly to `config.json`. Names use 1–64 lowercase
letters, digits, or internal `.`, `_`, and `-` characters. The reserved IDs are
`opencode-go`, `opencode-zen`, `chatgpt`, and `fake`.

For a keyless local endpoint that you do not need to save, pass its settings for
one launch:

```sh
snow --provider openai-compatible \
  --base-url http://127.0.0.1:8080/v1 \
  --model local-model
```

An endpoint can be an API root or a full URL ending in `/responses` or
`/chat/completions`. Pass `--model MODEL` when model discovery does not return a
usable model.

Use `snow login my-provider` to replace the stored key for an existing named
profile. You may also pass `--api-key` for one process. `OPENAI_API_KEY` applies
only to the unnamed `openai-compatible` profile; named profiles do not inherit
it. Keyless gateways receive no `Authorization` header.

## Common commands

Use these commands with a provider ID or named profile:

```sh
snow login PROVIDER
snow auth check PROVIDER
snow logout PROVIDER
snow --provider PROVIDER
snow --provider PROVIDER --model MODEL
```

`auth check` reports whether Snow can resolve a credential. A keyless provider
such as anonymous `opencode-zen` or a local compatible endpoint can work even
when no credential is configured.

## Troubleshooting

- Confirm the provider ID or named profile passed to `--provider`.
- Run `snow auth check PROVIDER` when that provider requires a credential.
- Pass `--model MODEL` when the endpoint cannot supply a usable model list.
- Confirm that a compatible endpoint includes its expected API base path.
- Run `snow --help` or the relevant `snow login --help` command for the
  installed version.

[chatgpt-auth]: https://github.com/elmissouri16/snow-core/blob/main/docs/chatgpt-auth.md

## Related documents

- [Getting started](getting-started.md) — install Snow and run the first prompt.
- [Configuration](configuration.md) — set provider and model defaults.
- [ChatGPT authentication][chatgpt-auth] — detailed OAuth behavior and
  troubleshooting.
- [Security model](security.md) — understand credentials and process authority.
