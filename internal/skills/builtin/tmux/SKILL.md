---
name: tmux
description: Remote-control tmux sessions for interactive CLIs by sending keystrokes and scraping pane output.
metadata: {"emoji":"🧵","os":["darwin","linux"],"requires":{"bins":["tmux"]}}
---

# tmux Skill

Use tmux only when you need an interactive TTY. Prefer exec background mode for long-running, non-interactive tasks.

## Quickstart (isolated socket, exec tool)

```bash
SOCKET_DIR="${ORI_TMUX_SOCKET_DIR:-${TMPDIR:-/tmp}/ori-tmux-sockets}"
mkdir -p "$SOCKET_DIR"
SOCKET="$SOCKET_DIR/ori.sock"
SESSION=ori-go

tmux -S "$SOCKET" new -d -s "$SESSION" -n shell
tmux -S "$SOCKET" send-keys -t "$SESSION":0.0 -- 'python3 -q' Enter
tmux -S "$SOCKET" capture-pane -p -J -t "$SESSION":0.0 -S -200
```

## Socket convention

- Use `ORI_TMUX_SOCKET_DIR` environment variable.
- Default socket path: `"$ORI_TMUX_SOCKET_DIR/ori.sock"`.

## Targeting panes and naming

- Target format: `session:window.pane` (defaults to `:0.0`).
- Keep names short; avoid spaces.
- Inspect: `tmux -S "$SOCKET" list-sessions`, `tmux -S "$SOCKET" list-panes -a`.

## Sending input safely

- Prefer literal sends: `tmux -S "$SOCKET" send-keys -t target -l -- "$cmd"`.
- Control keys: `tmux -S "$SOCKET" send-keys -t target C-c`.

## Watching output

- Capture recent history: `tmux -S "$SOCKET" capture-pane -p -J -t target -S -200`.

## Cleanup

- Kill a session: `tmux -S "$SOCKET" kill-session -t "$SESSION"`.
- Kill all sessions on a socket: `tmux -S "$SOCKET" list-sessions -F '#{session_name}' | xargs -r -n1 tmux -S "$SOCKET" kill-session -t`.
- Remove everything on the private socket: `tmux -S "$SOCKET" kill-server`.
