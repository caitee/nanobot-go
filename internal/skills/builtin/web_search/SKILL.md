---
name: web_search
description: Search the web for current information, news, and facts.
metadata: {"emoji":"🔍","requires":{"bins":["curl"]}}
---

# Web Search

Use web search to find current information, news, and facts.

## When to use

Use this skill when the user asks:
- "search for …"
- "what is the latest news about …"
- "find information about …"
- "look up …"

## Quick Search

Use a search engine's "I'm Feeling Lucky" redirect for quick answers:

```bash
curl -sL "https://duckduckgo.com/?q=your+search+query&format=json"
```

Or use a search API for more control:

```bash
curl -s "https://api.duckduckgo.com/?q=your+search+query&format=json&no_html=1"
```

## Web Search Tips

- URL-encode spaces: `q=search+term` or `q=search%20term`
- Use quotes for exact phrases: `q="exact+phrase"`
- Add site restrictions: `q=term+site:example.com`
- Combine terms: `q=term1+AND+term2` or `q=term1+OR+term2`

## Fetching Web Pages

For simple page fetching:

```bash
curl -sL "https://example.com" | head -n 100
```

For HTML to text conversion:

```bash
curl -sL "https://example.com" | lynx -dump -stdin
```

Or use `w3m`, `elinks`, or similar tools if available.

## Notes

- Always prefer authoritative sources
- Verify information from multiple sources when important
- Be mindful of rate limiting on search APIs
