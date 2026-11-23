# herdr-prompt-history

A [Herdr](https://herdr.dev) plugin that lists every prompt you sent in a Claude Code session and shows the report Claude gave for each one. It opens as a popup over the Claude pane and never touches the chat itself.

```
 Prompt history · 6 prompts                                   Claude: working
╭──────────────────────────────╮╭────────────────────────────────────────────╮
│ #1   19:53  lets brainstorm… ││ #2 · Mon 23 Nov 19:57                      │
│      17 tools · 3m36s        ││ with herdr we can set key binding for this │
│▌#2   19:57  ↳ with herdr we… ││ 1 tool · 34s · final answer                │
│▌     1 tool · 34s            ││ ────────────────────────────────────────── │
│ #3   20:02  ↳ I want to hav… ││ Yes, confirmed: Herdr keybindings work…    │
╰──────────────────────────────╯╰────────────────────────────────────────────╯
```

## What it shows

- **Prompts**: what you typed at the prompt, slash commands Claude answered, messages you sent while Claude was working (`↳`), and messages you typed when declining a tool call (`↳`).
- **Report**: Claude's final answer for the selected prompt, rendered as markdown. `t` switches to the full turn: every text block plus the tool calls in between.
- **Stats**: tool calls, files edited and duration per prompt.
- **Live**: the list follows the session while the popup is open.

## Install

Needs Herdr 0.9 or later and Go.

```sh
herdr plugin link /path/to/this/repo   # local checkout
bash herdr/install.sh                  # builds bin/herdr-prompt-history
```

Bind the action in `~/.config/herdr/config.toml`, then run `herdr server reload-config`:

```toml
[[keys.command]]
key = "prefix+m"
type = "plugin_action"
command = "prompt-history.open"
description = "prompt history"
```

The key opens the history of the Claude pane you are focused on. If focus is on a neighbour, such as an editor sidebar, it uses the tab's Claude pane when there is exactly one.

## Keys

| Key | Does |
| --- | --- |
| `j` / `k`, arrows, `g` / `G` | move in the focused pane |
| `tab`, `h` / `l` | switch between the list and the report |
| `j` / `k` in the report | scroll it line by line |
| `d` / `u`, `ctrl+d` / `ctrl+u` | scroll the report half a page, from either pane |
| `ctrl+f` / `ctrl+b`, `pgdn` / `pgup` | scroll the report a page |
| mouse wheel | scroll the pane under the pointer; a click focuses a pane or selects a prompt |
| `/` | filter prompts by text; `enter` keeps the filter, `esc` clears it |
| `t` | final answer / full turn |
| `enter` or `o` | open the report in nvim; `:q` returns to the list |
| `y` | copy the report |
| `q` / `esc` | close |

Below 100 columns the popup shows one pane at a time; `tab` switches.

## nvim

`enter` writes the report as a markdown file and opens it in plain `nvim <file>`, so your own config, plugins and keymaps load as usual. The file is a copy: editing it changes nothing in the session.

Optional settings go in `config.toml` in the plugin's config directory (`herdr plugin config-dir prompt-history`):

```toml
nvim_bin = "nvim"                    # editor binary
nvim_env = ["NVIM_APPNAME=myapp"]    # extra environment for it
```

Without nvim it falls back to `$VISUAL`, `$EDITOR`, then `vim`.

## How it works

Herdr knows the Claude session id of each pane. The plugin finds that session's transcript under `$CLAUDE_CONFIG_DIR/projects` or any `~/.claude*/projects` directory and reads it; it only ever reads that file. The transcript format is not documented by Claude Code, so a Claude Code update can break the parsing.

## Develop

```sh
go test ./...
go build -o bin/herdr-prompt-history ./cmd/herdr-prompt-history
bin/herdr-prompt-history dump --session <id>              # prompt list as text
bin/herdr-prompt-history dump --session <id> --report 2   # one report
bin/herdr-prompt-history view --session <id>              # the viewer, in any terminal
herdr plugin log list --plugin prompt-history             # action logs
```
