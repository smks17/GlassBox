# AGENTS.md — GlassBox

Read this file first. It is the whole entry point; open the other docs only when the task needs them.

## What GlassBox is

A browser-based virtual Linux: a Go engine (shell, in-memory VFS, users, pipes, fd table, process table, jobs, virtual network) compiled to WebAssembly, driven by a Next.js terminal. The same engine builds as a native CLI (`cmd/glass`). It is a **simulation**: host access must not implement simulated behaviour.

## Stack

- Go 1.25.4 or newer (see `go.mod`) — engine (`internal/engine/...`), adapters `cmd/glass` (native) and `cmd/wasm` (browser, `//go:build js && wasm`).
- Next.js 16 / React 19 / TypeScript — `web/`. Static export, deployed to Cloudflare Pages.
- FastAPI / SQLite — `backend/main.py` (session storage; see `backend/README.md`).
- WASI source is present; see `docs/WASI.md` for its current status and limits.

## Commands

```sh
go build ./cmd/... && go vet ./internal/engine/shell/ ./internal/engine/vfs/   # quick check
go build ./... && go vet ./... && go test ./...        # full check (see "Test gotchas" below)
GOOS=js GOARCH=wasm go build -o /dev/null ./cmd/wasm   # WASM target must keep compiling
gofmt -l cmd internal                                  # must print nothing
go run ./cmd/glass            # in-memory machine REPL;  -real = real disk;  -load host:path = copy a file in
GOOS=js GOARCH=wasm go build -o web/public/engine.wasm ./cmd/wasm  # local browser engine
cd web && npm run dev       # frontend; needs the browser engine above
cd web && npm run lint && npm run build                 # frontend checks
./deploy.sh                 # publishes frontend to Pages; FastAPI is deployed separately; explicit deployment authorization required
```

## Rules that must not be broken

1. **No host shortcuts.** `os`, `os/exec`, `net`, `syscall`, `unsafe`, `plugin` are forbidden in simulated engine code. Existing `osfs`, `external`, `hostnet` adapters are quarantined for native use; do not extend host access without discussing it. `syscall/js` lives only in `cmd/wasm`. Existing violations are recorded in `docs/DECISIONS.md`; they are not precedents.
2. **Keep layers apart.** Frontend = presentation; `cmd/wasm` = thin adapter; logic lives in Go. Never re-implement shell/VFS behaviour in TypeScript.
3. **Every VFS operation goes through `checkPerm`.** New inodes carry an `Ownership`.
4. **Inode lock rule:** never hold one inode's lock while taking another (use `lockPair`). `.` and `..` are real entries, so walk trees through `Entries()`.
5. **One foreground command invocation per session at a time.** The caller must serialize it; session/shell mutation has no general lock. The current browser bridge is synchronous; preserve serialization when adding asynchronous execution.
6. **A change to a JS global in `cmd/wasm` changes `web/lib/wasm-bridge.ts` in the same commit.**
7. **Do not start unrelated refactors.** Smallest coherent change, stub rather than delete.

## Conventions

- Implement everything as simply as possible while meeting the requirements. Prefer clear, direct code and existing tools; avoid unnecessary abstractions, dependencies, and speculative features. Preserve correctness and required error handling when simplifying.
- Idiomatic Go; full, self-explanatory names (no `take`/`give`/`due` style abbreviations). Keep existing public names (`ProcessState`, `StopAction`, ...) when simplifying.
- Comments explain *why*, in full sentences; match the density of the file you are in.
- Errors: wrap sentinels (`fmt.Errorf("open %q: %w", ...)`), test with `errors.Is`. User errors never panic. Command errors print as `name: arg: error` on `ctx.Stderr` and return an exit code.
- New command: one file in `internal/engine/commands/`, type implementing `Name/Usage/Run`, `func init() { Register(X{}) }`. Read via `ctx.VFS.OpenDefault`; run loops check `ctx.Interrupted()`; pass copy errors through `signal.Signalled(err)`.
- Conversation, code, identifiers, comments and UI text: English. Write Persian documentation or task descriptions only when explicitly requested.

## Working agreement with this repo's owner

- **Never commit, amend, push or `git add -A` unprompted.** Stage one logical group, show `git diff --cached --stat` and a proposed message, wait for a yes.
- Do not commit test files unless asked.
- Show edits as visible per-file edits, not opaque scripts. Propose renames before applying them. State the plan and wait before destructive steps.
- For significant design/feature work or explicit pairing, use `.agents/skills/parrot-collaboration/SKILL.md`. Agree the design before coding; name the owner and verification for each piece. Invite the owner to implement educational sections first, offer hints, and pause at agreed review checkpoints. Explicit direct-implementation requests authorize only their stated scope.
- These participation checkpoints and Git rules take precedence over generic skill defaults for continuous execution, autonomous design decisions or automatic commits. Scale the process to the task; small authorized edits do not need a new design ceremony.
- Ask before installing dependencies or downloading third-party tools. Prefer existing tools and editable Mermaid in existing Markdown documents; do not add integrations speculatively.
- Consult `advisor_reviewer` before a major plan, on the second occurrence of the same error, and before declaring a long task done. If the role is unavailable in another environment, disclose that and agree a review alternative with the owner. Use other subagents only when authorized and useful for a bounded task; do not delegate away the owner's learning opportunity.
- Branches: `feature/*` from `develop`; PRs target `develop`.

## Test gotchas

- Tests are ignored by default, including `internal/engine/boundary_test.go`; do not change test tracking or commit tests without asking. Documentation is local and uncommitted except for the existing READMEs. `.wasm` files and `Thirdparty/` remain ignored.
- Check branch, status and actual files before trusting a historical failure report. Preserve ignored tests and local artifacts; ask before moving or deleting them. Some tests run host tools or open localhost sockets, so inspect the relevant tests before running them.
- Run checks appropriate to the change and report actual results. Current build/test health is not established by old documentation. Avoid repository-wide toolchain scans under `Thirdparty/`.

## Where to look next

| Need | File |
|---|---|
| Explore the generated codebase knowledge graph | `graphify-out/graph.html` |
| Ask questions of the graph data | `graphify-out/graph.json` |

The graph is local to this repository and can be opened directly in a browser. In a future session, use `$graphify` to query or rebuild it. Graph outputs are generated artifacts and may be absent from a fresh checkout.

After completing a significant feature, update the relevant project documentation and rebuild the knowledge graph so it reflects the current codebase.
