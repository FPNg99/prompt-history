# prompt-history

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
- **Reports**: one prompt often gets several reports, since Claude writes a message, runs tools, then writes again. The latest report shows by default. The selected prompt lists all of its reports, and `ctrl+j` / `ctrl+k` or a number key picks another one.
- **Full turn**: `t` shows every report of the prompt in order, with the tool calls in between.
- **Model**: each prompt shows the model it was sent to, in the list and in the report header. The header also names the model that wrote the shown report. In full-turn view it lists every model that took part, which is more than one if you switched model part-way.
- **Stats**: tool calls, files edited, number of reports, tokens and duration per prompt.
- **Tokens**: the report header breaks a prompt's token use into input (with the share read from cache) and output, summed over every request Claude made to answer it. Subagent usage is not included.
- **Live**: the list follows the session while the popup is open.

## Install

Needs Herdr 0.9 or later.

The install step downloads a prebuilt binary for macOS (Apple silicon or Intel) and Linux (x64 or arm64) from this repo's releases and verifies its checksum. If [Go](https://go.dev/dl) is installed, it builds from source instead.

```sh
herdr plugin install FPNg99/prompt-history
```

To work on it from a local checkout instead:

```sh
herdr plugin link /path/to/this/repo
bash herdr/install.sh                  # builds bin/prompt-history
```

Bind the action in `~/.config/herdr/config.toml`, then run `herdr server reload-config`:

```toml
[[keys.command]]
key = "prefix+m"
type = "plugin_action"
command = "prompt-history.open"
description = "prompt history"

[[keys.command]]
key = "prefix+shift+m"
type = "plugin_action"
command = "prompt-history.open-tab"
description = "prompt history tab"
```

There are two ways to open it:

- **Popup** (`prompt-history.open`): floats over the current tab for a quick look; `q` or `esc` closes it.
- **Tab** (`prompt-history.open-tab`): a tab of its own, labelled `history: <session name>` after the Claude session it shows, that stays open so you can switch between Claude and the history. Pressing the key again goes back to the tab already open for that session. `q` closes it; `esc` does not.

Both open the history of the Claude pane you are focused on. If focus is on a neighbour, such as an editor sidebar, it uses the tab's Claude pane when there is exactly one.

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
| `ctrl+k` / `ctrl+j` | previous / next report of the selected prompt (also `[` / `]`, `p` / `n`) |
| `1`–`9` | jump to that report |
| `t` | one report / full turn |
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

## Releases

Every merge to `main` runs the tests, then builds the binaries and attaches them to the GitHub release named after the `version` in `herdr-plugin.toml`. Bumping that version creates a new release; a merge that keeps it replaces the binaries of the current one, so they always match `main`. Pull requests run the tests only.

## Develop

```sh
go test ./...
go build -o bin/prompt-history ./cmd/prompt-history
bin/prompt-history dump --session <id>              # prompt list as text
bin/prompt-history dump --session <id> --report 2   # prompt 2's latest report
bin/prompt-history dump --session <id> --report 2 --part 1   # its first report
bin/prompt-history view --session <id>              # the viewer, in any terminal
herdr plugin log list --plugin prompt-history             # action logs
```
