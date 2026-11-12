# legacy

The Python implementation Browser Foreman started as: an HTTP server (`server/`), the MCP server for AI assistants
(`mcp/`), shared helpers (`lib/`) and their tests (`tests/`). It is kept as the reference for behaviour while each
part is ported to the Go binary in `src/` (see `docs/roadmap.md`), and is removed, through its own pull request, once
the Go binary covers everything it does.

It is not built or released, and nothing in `src/` imports it.
