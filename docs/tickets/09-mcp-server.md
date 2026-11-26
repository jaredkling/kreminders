# Ticket 09 — MCP server

**Depends on:** 08
**Effort:** two sessions (mostly learning the SDK)
**Read first:** [`../mcp.md`](../mcp.md) (all of it — it is the contract), [`../design.md`](../design.md) §9

## Goal

`POST /mcp` serves the thirteen tools from `mcp.md` over streamable HTTP,
authenticated with a bearer token, backed by the same service layer as the web
pages. With `KREM_MCP_TOKEN` unset the route does not exist. An agent on the LAN
can manage every task you own.

## Why now

The service layer, the state machine and the delivery are all real; the tools are
translations. If you find yourself writing logic in a tool handler, that's a
signal the service layer is missing something — which is the most useful
feedback this ticket gives you.

## What you'll learn

| Concept | Where to look |
|---|---|
| MCP protocol and transports | [modelcontextprotocol.io](https://modelcontextprotocol.io), the SDK's `docs/` folder |
| Go SDK server API | `go doc github.com/modelcontextprotocol/go-sdk/mcp`, `mcp.NewServer`, `mcp.AddTool` |
| Streamable HTTP handler | SDK `examples/http`, `mcp.NewStreamableHTTPHandler` |
| Bearer auth middleware | SDK `auth.RequireBearerToken`, `mcp/auth` |
| Generics in practice | [Go blog: generics](https://go.dev/blog/intro-generics) — you'll use the SDK's generic `AddTool`, not write your own |
| Struct tags for JSON schema | SDK docs on `jsonschema` tags |
| Tool errors vs protocol errors | MCP spec section on tool results |

## Scope

**In**

- An `internal/mcp` package registering all tools from `mcp.md` §3 with the
  exact names, arguments and response shapes.
- Bearer auth middleware wrapping the streamable handler; constant-time
  verification (the SDK helper is fine).
- Wiring in `serve`: mount at `/mcp` on the existing listener; when the token is
  empty, don't mount it at all (404, not 401).
- Error mapping: domain not-found/conflict/invalid → tool error with a helpful
  message; malformed arguments → the SDK's normal input validation.
- Timeouts: a per-call context deadline sensible for DB work.
- A test that drives the server with the SDK's own client over HTTP.

**Out**

- Resources, prompts, sampling, elicitation. Tools only.
- stdio transport.
- Any new business rules; defaults (plans, grace) must come from the shared
  service layer used by the web path too.

## Acceptance criteria

```sh
curl -i -X POST https://kreminders.nb.kling.dev/mcp \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

- [ ] Without `Authorization` the response is 401; with a wrong token, 401.
      Neither response reveals whether the token was close.
- [ ] `KREM_MCP_TOKEN` unset: the path is 404 and the startup log says MCP is
      disabled.
- [ ] With a token, `initialize` succeeds and `tools/list` returns exactly the
      thirteen names from `mcp.md`, with input schemas that match the argument
      tables.
- [ ] Through the SDK client (in a test, against `httptest`): `create_task` →
      `list_tasks` → `list_open` → `complete_task` → `history` produces the
      documented response fields, using the four example tasks' values.
- [ ] `complete_task` by slug when nothing is open returns a tool error that
      lists the actual open tasks (not a stack trace, not a SQL error).
- [ ] Double `complete_task` returns `was_already_resolved: true` and the same
      `resolved_at`.
- [ ] Creating two tasks with the same slug returns a tool error mentioning the
      slug, not a constraint violation.
- [ ] `confirm_url` returns a URL that ticket 08's handlers accept and that
      expires when documented.
- [ ] Unknown arguments are rejected by input validation rather than ignored.
- [ ] No tool handler contains SQL or plan arithmetic; grep for `SELECT` under
      `internal/mcp` and find nothing.
- [ ] `go test ./internal/mcp -race` passes offline, and your agent connects
      with the config from `mcp.md` §1.

## Suggested approach

1. Read the SDK's HTTP example end to end before writing a line. Then stand up a
   handler with **zero** tools and confirm `initialize` + `tools/list` work with
   `curl`. Getting the transport right in isolation saves an hour of confusion
   later.
2. Add auth and test 401/404 before any tool exists.
3. Add tools in this order, testing after each: `list_tasks` (read-only, no
   arguments), `get_task`, `list_open`, then the mutations. Reads teach you the
   result shape with no risk.
4. For each tool, write the description and argument docs as if an agent with no
   context is reading them — because that's the actual consumer. Note which
   arguments are mutually exclusive and say so in the description.
5. Decide argument validation: what does the SDK already do (types, required
   fields) and what's left (semantic rules like "slug or id")? Keep semantic
   validation in the service layer so the web path gets it too.
6. Test with the SDK client, not by hand-crafting JSON-RPC: it exercises session
   handling and catches shape mistakes your own parser would forgive.

## Questions to answer

- Which tools should return a confirmable link, and what stops an agent from
  logging that link somewhere public? (Read `mcp.md` §6, then decide whether
  `confirm_url` is a good idea anyway — the design says yes; do you agree?)
- `create_task` has documented defaults for `phases`. Where do they live so the
  web path and MCP can't drift?
- Should `list_open` include future advance-notice cycles? What does `mcp.md`
  promise, and does your implementation match?
- How does a tool signal "you asked for something impossible" versus "the server
  broke"? Which of those is `isError: true`?
- What happens if an agent calls `complete_task` for a cycle that a tick is
  sending right now? (You solved this in 07; is the MCP path reusing it?)
- What is the worst thing a holder of the bearer token can do, and does the
  response text anywhere make that easier?

## Pitfalls

- Implementing `tools/call` by hand instead of using the SDK's generic helper;
  you'll get schema and content shapes subtly wrong.
- Returning a Go error for a domain failure, which the client sees as a protocol
  error rather than a tool result.
- Using `context.Background()` inside a handler (loses cancellation) or a
  deadline so long that a stuck DB call holds a connection forever.
- Creating a new `mcp.Server` per request without need, or sharing one while
  mutating per-connection state.
- Logging full request bodies (they contain notes and confirm links).
- Duplicating validation in the tool layer so the web path accepts input the
  MCP path rejects (or vice versa).
- Forgetting to mount the handler at the exact path the proxy and client config
  use (`/mcp`, no trailing slash).
- Assuming the agent will pass RFC 3339; it may pass a bare date. The design says
  accept both — make the error message tell the caller which format it got.

## Testing checklist

- [ ] Auth matrix: no header, wrong scheme, wrong token, valid token.
- [ ] Disabled endpoint: 404.
- [ ] `tools/list` golden test of names (catches accidental renames).
- [ ] Per-tool argument validation: missing required, unknown field, wrong type.
- [ ] Full lifecycle through the SDK client with a temp DB.
- [ ] Idempotency and error-message assertions from the acceptance list.
- [ ] `confirm_url` round-trip against the web handlers.
- [ ] `go test ./... -race -count=1`; then one real call from your agent,
      including the "I did my contacts" dialogue from `mcp.md` §4.

## When you're stuck

If `curl` gets 406, look at the `Accept` header — streamable HTTP wants both
`application/json` and `text/event-stream` listed. If the SDK client hangs,
check the session id: the client sends `Mcp-Session-Id` after `initialize`, and a
proxy or middleware that strips headers will break it. If tools appear in
`tools/list` but calls fail validation, print the generated schema via
`tools/list` and compare it with your struct tags — the schema is the truth the
client sees, not your Go types.
