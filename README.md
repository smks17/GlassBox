# glassBox

A virtual Linux environment that runs in the browser and CLI. It is a simulation, not a real operating system: the shell, filesystem, users, pipes and processes are all written in Go.


## What it has

- A shell with pipelines, redirection, `if`/loops/`for`, functions, `source`, globbing and job control (`&`, `jobs`, `fg`, `bg`, Ctrl-Z, Ctrl-C).
- An in-memory filesystem with owners, groups, permission bits, hard links and `/dev` devices.
- Users and login (`/etc/passwd`, `/etc/shadow`, `su`).
- A process table with PIDs, signals and `ps`/`kill`.
- About 20 commands: `cat`, `cd`, `chmod`, `chown`, `cp`, `cut`, `date`, `echo`, `ls`, `mkdir`, `mv`, `rm`, `wc`, and more. Type `help` to list them.
- Session save and restore, with an optional Cloudflare Worker backend to store sessions.
- Network feature

## Try it

You need Go 1.25 or newer.

```sh
go run ./cmd/glass               # an in-memory machine
go run ./cmd/glass -real         # the same shell on your real disk
go run ./cmd/glass script.sh a b   # run a script with arguments
go run ./cmd/glass --help          # see usage
```

### In the browser

You also need Node 22 or newer.

```sh
GOOS=js GOARCH=wasm go build -o web/public/engine.wasm ./cmd/wasm
cd web
npm install
npm run dev
```

`./deploy.sh` builds the engine, then deploys the Worker in `backend/` and the static site to Cloudflare Pages. It needs `npx wrangler login` first.

## Simulated machine runtime

The CLI and browser use `internal/engine.Machine` through the compatible `App` API.
Go hosts can manage an individual machine explicitly:

```go
machine, err := engine.NewMachine("lab-a")
if err != nil { return err }
if err := machine.Start(); err != nil { return err }
result := machine.Execute("echo hello > greeting")
_ = result
if err := machine.Stop(ctx); err != nil { return err }
if err := machine.Start(); err != nil { return err } // greeting still exists
```

A restart preserves files, identity, and virtual clock settings, and creates a fresh
shell, process table, and loopback network. Shell variables, history, working directory,
login identity, external interfaces, and published ports reset. Reattach external
networking after each boot. Machine IDs are assigned by the caller; uniqueness is
not enforced across instances.

`Stop(ctx)` cancels machine work and waits for actual workers and network cleanup.
A timeout leaves the machine `stopping` until cleanup finishes; another `Stop` can
wait again. `Info()` remains available during execution. Commands and session changes
are serialized per machine; separate machines can execute independently. Snapshot
restore replaces the session and network after cleaning up old background work.
Snapshots capture filesystem and shell state, not running processes or machine IDs.

Hosts can share one `wazero.CompilationCache` by passing it to each machine's
`SetWasiCache` before executing commands. The cache survives restart and restore;
stop all borrowing machines before closing it. Every program execution gets its
own wazero runtime and guest memory. NewApp and NewAppOn create an in-memory cache
owned by their Machine; NewMachine leaves cache setup to its caller. Machine.Close stops the machine and
closes that default cache. Use Stop for restart and Close for final cleanup.
Caller-supplied caches remain caller-owned.

The browser exposes `shutdown(): Promise<boolean>` for explicit cleanup. Tab termination
does not guarantee that cleanup finishes. There is no cluster manager or systemd dependency.

## Check your changes

```sh
go build ./... && go vet ./... && go test ./...
GOOS=js GOARCH=wasm go build -o /dev/null ./cmd/wasm   # the engine must still compile to WASM
gofmt -l .                                             # must print nothing
```

## How it is organised

```text
web/          Next.js terminal (presentation only)
cmd/wasm      thin browser adapter
cmd/glass     native command line
internal/engine/
  shell/      lexer, parser, executor, jobs
  commands/   one file per command
  filesystem/ filesystem interface
  vfs/        in-memory filesystem
  ofs/        real disk filesystem
  stream/     files, descriptors, pipes
  proc/       process table and signals
  user/       accounts and passwords
  network/    network and dns
backend/      Cloudflare Worker that stores sessions
```

The rule that shapes everything: the engine must never use the host operating system to implement a feature, and it must always compile to WebAssembly.

## TODO
- [ ] Add command snapshot inside cli like browser
- [ ] Run wasm program inside glassBox
- [ ] Add package and plugin manager
- [ ] Complete remain engine features inside frontend
- [ ] Rewrite backend with Python or Go
- [ ] Unittest
- [ ] Implement mount
- [ ] Implement systemd
- [ ] Resource model (memory, CPU), symlinks, atomic Move.


## License

See [LICENSE](LICENSE).
