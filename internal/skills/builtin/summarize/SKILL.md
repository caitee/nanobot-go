---
name: summarize
description: Summarize or extract text/transcripts from URLs, podcasts, and local files.
homepage: https://summarize.sh
metadata: {"emoji":"🧾","requires":{"bins":["summarize"]}}
---

# Summarize

Fast CLI to summarize URLs, local files, and YouTube links.

## When to use (trigger phrases)

Use this skill when the user asks any of:
- "what's this link/video about?"
- "summarize this URL/article"
- "transcribe this YouTube/video"

## Quick start

```bash
summarize "https://example.com" --model openai/gpt-4
summarize "/path/to/file.pdf" --model openai/gpt-4
summarize "https://youtu.be/dQw4w9WgXcQ" --youtube auto
```

## YouTube: summary vs transcript

Best-effort transcript (URLs only):

```bash
summarize "https://youtu.be/dQw4w9WgXcQ" --youtube auto --extract-only
```

## Model + keys

Set the API key for your chosen provider:
- OpenAI: `OPENAI_API_KEY`
- Anthropic: `ANTHROPIC_API_KEY`

## Useful flags

- `--length short|medium|long|xl|xxl|<chars>`
- `--extract-only` (URLs only)
- `--json` (machine readable)
