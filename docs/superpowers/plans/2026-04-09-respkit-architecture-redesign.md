# Respkit Architecture Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current handler/mux-driven server path with a server-centered architecture built around session read/write loops, a global dispatcher, `basekit` scope-driven request memory, and `basekit` logging.

**Architecture:** `Server` owns listener, dispatcher, and session registry. Each accepted connection becomes a `Session` with `readLoop` and `writeLoop`; requests are parsed into `protocol.RespValue`, wrapped with a `basekit` mempool `Scope`, scheduled through a dispatcher with a scheduler goroutine plus state queues, executed as `command.Command`, then written back and flushed before releasing the request scope.

**Tech Stack:** Go, RESP protocol, `basekit` log module, `basekit` mempool scope module, standard `go test`, `go test -race`

---

## File Structure

### Files to modify
- `go.mod` — add `basekit` dependency
- `go.sum` — dependency checksum updates
- `respkit.go` — redefine public API around `Server`, `Config`, and new runtime types; remove `Handler`/`CommandFactory`
- `server.go` — rebuild server lifecycle, accept loop, dispatcher startup/shutdown, session registration
- `internal/conn/conn.go` — rebuild as thin RESP I/O boundary using external `Scope`
- `internal/protocol/types.go` — keep `RespValue`, add any helpers needed by new command path
- `internal/protocol/types_test.go` — update tests for new protocol helpers as needed
- `internal/session/session.go` — rebuild session lifecycle, read/write loops, inflight control, scoped response release
- `server_test.go` — replace lifecycle tests to match new architecture
- `example/basic-server.go` — update example to new API
- `example/basic_server_test.go` — update example tests to new API

### Files to create
- `internal/dispatcher/dispatcher.go` — dispatcher types, scheduler, worker lifecycle
- `internal/dispatcher/dispatcher_test.go` — dispatcher scheduler/queue tests
- `internal/command/command.go` — `Command` interface, exec context, build entrypoint
- `internal/command/basic.go` — `PING`, `ECHO`, unknown command implementations
- `internal/command/command_test.go` — command build/execute tests
- `internal/session/session_test.go` — session read/write/inflight/scope tests
- `internal/conn/conn_test.go` — conn read/write/flush tests with external scope

### Files to delete
- `mux.go`

### Existing code to remove during edits
- `Handler`, `HandlerFunc`, `CommandFactory`, `CommandFactoryFunc` definitions in `respkit.go`
- old handler/factory dispatch path in `server.go`

---

### Task 1: Add basekit dependency and config surface

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `respkit.go`
- Test: `go test ./...`

- [ ] **Step 1: Add the failing public-API test for new config surface**

```go
func TestDefaultConfigPreservesDispatcherSettings(t *testing.T) {
	cfg := defaultConfig(&Config{
		DispatcherWorkers:    4,
		QueueSize:            64,
		MaxInFlightPerSession: 8,
	})

	if cfg.DispatcherWorkers != 4 {
		t.Fatalf("DispatcherWorkers = %d, want 4", cfg.DispatcherWorkers)
	}
	if cfg.QueueSize != 64 {
		t.Fatalf("QueueSize = %d, want 64", cfg.QueueSize)
	}
	if cfg.MaxInFlightPerSession != 8 {
		t.Fatalf("MaxInFlightPerSession = %d, want 8", cfg.MaxInFlightPerSession)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestDefaultConfigPreservesDispatcherSettings`
Expected: FAIL with unknown `DispatcherWorkers`, `QueueSize`, or `MaxInFlightPerSession` fields.

- [ ] **Step 3: Add basekit dependency**

```bash
go get /Users/yanjie/data/code/local/basekit
```

Then ensure `go.mod` includes the basekit module requirement used by later code.

- [ ] **Step 4: Rewrite the public config and API surface in `respkit.go`**

```go
type Config struct {
	Addr                  string
	Network               string
	ReadBufferSize        int
	WriteBufferSize       int
	DispatcherWorkers     int
	QueueSize             int
	MaxInFlightPerSession int
	IdleTimeout           time.Duration
	MemPool               mempool.Provider
	Logger                log.Logger
}

type ScopedRequest struct {
	Session *session.Session
	Request protocol.RespValue
	Scope   *mempool.Scope
}

type ScopedResponse struct {
	Session  *session.Session
	Response protocol.RespValue
	Scope    *mempool.Scope
}
```

Also remove these obsolete public types from `respkit.go`:

```go
type Handler interface { Handle(ctx *Context) error }
type HandlerFunc func(ctx *Context) error
type CommandFactory interface {
	CreateCommand(name string, raw []byte, args [][]byte) (Command, error)
}
```

- [ ] **Step 5: Implement sensible defaults in `defaultConfig`**

```go
func defaultConfig(config *Config) *Config {
	cfg := *config
	if cfg.Addr == "" {
		cfg.Addr = ":6379"
	}
	if cfg.Network == "" {
		cfg.Network = "tcp"
	}
	if cfg.ReadBufferSize == 0 {
		cfg.ReadBufferSize = 16384
	}
	if cfg.WriteBufferSize == 0 {
		cfg.WriteBufferSize = 4096
	}
	if cfg.DispatcherWorkers == 0 {
		cfg.DispatcherWorkers = 1
	}
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 1024
	}
	if cfg.MaxInFlightPerSession == 0 {
		cfg.MaxInFlightPerSession = 32
	}
	if cfg.MemPool == nil {
		cfg.MemPool = mempool.NewProvider()
	}
	if cfg.Logger == nil {
		cfg.Logger = log.NewNopLogger()
	}
	return &cfg
}
```

- [ ] **Step 6: Run the targeted test to verify it passes**

Run: `go test ./... -run TestDefaultConfigPreservesDispatcherSettings`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum respkit.go
git commit -m "refactor: add dispatcher and scope config surface"
```

### Task 2: Rebuild connection I/O around external Scope

**Files:**
- Modify: `internal/conn/conn.go`
- Create: `internal/conn/conn_test.go`
- Modify: `internal/protocol/types.go`
- Test: `go test ./internal/conn ./internal/protocol -run 'TestConn|TestRespValue'`

- [ ] **Step 1: Write the failing conn test for scope-driven read/write**

```go
func TestConnReadUsesExternalScopeAndWriteFlushesResponse(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	go func() {
		_, _ = client.Write([]byte("*1\r\n$4\r\nPING\r\n"))
	}()

	provider := mempool.NewProvider()
	scope := provider.NewScope()
	defer scope.Release()

	c := NewConn(server, protocol.NewReader(server), protocol.NewWriter(server))
	value, err := c.Read(scope)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got := value.CommandName(); got != "ping" {
		t.Fatalf("CommandName() = %q, want ping", got)
	}

	if err := c.Write(protocol.SimpleString("PONG")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := c.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/conn ./internal/protocol -run TestConnReadUsesExternalScopeAndWriteFlushesResponse`
Expected: FAIL because `Conn.Read(scope)` and RESP reader/writer APIs do not yet exist.

- [ ] **Step 3: Add protocol reader/writer helpers needed by the thin conn**

```go
type Reader struct {
	r io.Reader
}

func NewReader(r io.Reader) *Reader { return &Reader{r: r} }

func (r *Reader) ReadValue(scope *mempool.Scope, buf []byte) (RespValue, error) {
	// read bytes into scope-backed buffer, parse one RESP value, return it
}

type Writer struct {
	w io.Writer
	buf bytes.Buffer
}

func NewWriter(w io.Writer) *Writer { return &Writer{w: w} }

func (w *Writer) AppendValue(v RespValue) error {
	// encode RespValue into buffered writer
}

func (w *Writer) Flush() error {
	_, err := w.w.Write(w.buf.Bytes())
	w.buf.Reset()
	return err
}
```

- [ ] **Step 4: Rewrite `internal/conn/conn.go` as a thin scope-driven boundary**

```go
type Conn struct {
	netConn net.Conn
	reader  *protocol.Reader
	writer  *protocol.Writer
}

func NewConn(netConn net.Conn, reader *protocol.Reader, writer *protocol.Writer) *Conn {
	return &Conn{netConn: netConn, reader: reader, writer: writer}
}

func (c *Conn) Read(scope *mempool.Scope) (protocol.RespValue, error) {
	buf := scope.Bytes(0, 16<<10)
	return c.reader.ReadValue(scope, buf)
}

func (c *Conn) Write(v protocol.RespValue) error {
	return c.writer.AppendValue(v)
}

func (c *Conn) Flush() error {
	return c.writer.Flush()
}
```

Keep only `Close`, `RemoteAddr`, `SetReadDeadline`, and `SetDeadline` as thin pass-throughs.

- [ ] **Step 5: Run the targeted conn/protocol tests**

Run: `go test ./internal/conn ./internal/protocol -run 'TestConn|TestRespValue'`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/conn/conn.go internal/conn/conn_test.go internal/protocol/types.go internal/protocol/types_test.go
git commit -m "refactor: make conn scope-driven io boundary"
```

### Task 3: Add command package and basic command execution

**Files:**
- Create: `internal/command/command.go`
- Create: `internal/command/basic.go`
- Create: `internal/command/command_test.go`
- Test: `go test ./internal/command -run TestBuildCommand`

- [ ] **Step 1: Write the failing command-build tests**

```go
func TestBuildCommandReturnsPingCommand(t *testing.T) {
	cmd, err := BuildCommand(protocol.ArrayOf(protocol.BulkFromString("PING")))
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}
	if _, ok := cmd.(*PingCommand); !ok {
		t.Fatalf("BuildCommand() returned %T, want *PingCommand", cmd)
	}
}

func TestBuildCommandReturnsUnknownCommandErrorResponse(t *testing.T) {
	cmd, err := BuildCommand(protocol.ArrayOf(protocol.BulkFromString("NOPE")))
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}
	resp, execErr := cmd.Execute(&ExecContext{})
	if execErr != nil {
		t.Fatalf("Execute() error = %v", execErr)
	}
	if resp.Type != protocol.TypeError {
		t.Fatalf("response type = %v, want error", resp.Type)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/command -run TestBuildCommand`
Expected: FAIL because package and symbols do not exist.

- [ ] **Step 3: Create the command interface and execution context**

```go
type ExecContext struct {
	Session *session.Session
	Scope   *mempool.Scope
	Logger  log.Logger
}

type Command interface {
	Execute(*ExecContext) (protocol.RespValue, error)
}

func BuildCommand(request protocol.RespValue) (Command, error) {
	switch request.CommandName() {
	case "ping":
		return &PingCommand{Request: request}, nil
	case "echo":
		return &EchoCommand{Request: request}, nil
	default:
		return &UnknownCommand{Name: request.CommandName()}, nil
	}
}
```

- [ ] **Step 4: Implement the minimal basic commands**

```go
type PingCommand struct { Request protocol.RespValue }

func (c *PingCommand) Execute(*ExecContext) (protocol.RespValue, error) {
	return protocol.SimpleString("PONG"), nil
}

type EchoCommand struct { Request protocol.RespValue }

func (c *EchoCommand) Execute(*ExecContext) (protocol.RespValue, error) {
	args := c.Request.CommandArgs()
	if len(args) == 0 {
		return protocol.Error("ERR wrong number of arguments for 'echo' command"), nil
	}
	return protocol.BulkBytes(args[0]), nil
}

type UnknownCommand struct { Name string }

func (c *UnknownCommand) Execute(*ExecContext) (protocol.RespValue, error) {
	return protocol.Error("ERR unknown command '" + c.Name + "'"), nil
}
```

- [ ] **Step 5: Run command tests**

Run: `go test ./internal/command -run TestBuildCommand`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/command/command.go internal/command/basic.go internal/command/command_test.go
git commit -m "feat: add command execution package"
```

### Task 4: Build dispatcher with scheduler and state queues

**Files:**
- Create: `internal/dispatcher/dispatcher.go`
- Create: `internal/dispatcher/dispatcher_test.go`
- Test: `go test ./internal/dispatcher -run TestDispatcher`

- [ ] **Step 1: Write the failing dispatcher test for scheduler path**

```go
func TestDispatcherMovesRequestFromIncomingToWorkerAndBack(t *testing.T) {
	provider := mempool.NewProvider()
	d := New(Config{Workers: 1, QueueSize: 8}, log.NewNopLogger())
	d.Start()
	defer d.CloseNow()

	sess := &session.Session{ID: 1}
	scope := provider.NewScope()
	respCh := make(chan respkit.ScopedResponse, 1)
	sess.SetResponseSink(respCh)

	req := respkit.ScopedRequest{
		Session: sess,
		Request: protocol.ArrayOf(protocol.BulkFromString("PING")),
		Scope:   scope,
	}

	if err := d.Submit(req); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	select {
	case got := <-respCh:
		if got.Response.Type != protocol.TypeSimpleString {
			t.Fatalf("response type = %v, want simple string", got.Response.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dispatcher -run TestDispatcherMovesRequestFromIncomingToWorkerAndBack`
Expected: FAIL because dispatcher package does not exist.

- [ ] **Step 3: Implement dispatcher skeleton with scheduler and queues**

```go
type Dispatcher struct {
	incomingQueue chan respkit.ScopedRequest
	readyQueue    chan respkit.ScopedRequest
	completeQueue chan respkit.ScopedResponse
	waitingQueue  []respkit.ScopedRequest
	logger        log.Logger
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

func (d *Dispatcher) Start() {
	d.wg.Add(1)
	go d.schedulerLoop()
	for i := 0; i < d.workerCount; i++ {
		d.wg.Add(1)
		go d.workerLoop()
	}
}
```

- [ ] **Step 4: Implement the scheduler loop with first-stage semantics**

```go
func (d *Dispatcher) schedulerLoop() {
	defer d.wg.Done()
	for {
		select {
		case <-d.stopCh:
			return
		case req := <-d.incomingQueue:
			d.readyQueue <- req
		case done := <-d.completeQueue:
			deliverResponse(done)
		}
	}
}
```

Use `logger` calls from the `basekit` log module at request enqueue, worker error, and stop events.

- [ ] **Step 5: Implement the worker loop against the command package**

```go
func (d *Dispatcher) workerLoop() {
	defer d.wg.Done()
	for {
		select {
		case <-d.stopCh:
			return
		case req := <-d.readyQueue:
			cmd, err := command.BuildCommand(req.Request)
			if err != nil {
				d.completeQueue <- respkit.ScopedResponse{Session: req.Session, Response: protocol.Error(err.Error()), Scope: req.Scope}
				continue
			}
			resp, execErr := cmd.Execute(&command.ExecContext{Session: req.Session, Scope: req.Scope, Logger: d.logger})
			if execErr != nil {
				resp = protocol.Error(execErr.Error())
			}
			d.completeQueue <- respkit.ScopedResponse{Session: req.Session, Response: resp, Scope: req.Scope}
		}
	}
}
```

- [ ] **Step 6: Run dispatcher tests**

Run: `go test ./internal/dispatcher -run TestDispatcher`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/dispatcher/dispatcher.go internal/dispatcher/dispatcher_test.go
git commit -m "feat: add scheduler-based dispatcher"
```

### Task 5: Rebuild session with read/write loops, inflight control, and scope release

**Files:**
- Modify: `internal/session/session.go`
- Create: `internal/session/session_test.go`
- Test: `go test ./internal/session -run TestSession`

- [ ] **Step 1: Write the failing session test for inflight control and scope release**

```go
func TestSessionReleasesScopeAfterFlush(t *testing.T) {
	provider := mempool.NewProvider()
	scope := provider.NewScope()
	conn := newStubConn()
	dispatcher := newStubDispatcher(protocol.SimpleString("PONG"))

	sess := NewSession(1, conn, dispatcher, 1)
	sess.Start()
	defer sess.CloseNow()

	conn.QueueRead(scope, protocol.ArrayOf(protocol.BulkFromString("PING")))

	waitFor(func() bool { return conn.FlushCount() == 1 })
	if !scope.IsReleased() {
		t.Fatal("scope was not released after flush")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session -run TestSessionReleasesScopeAfterFlush`
Expected: FAIL because new session API and scope lifecycle do not yet exist.

- [ ] **Step 3: Redefine the session type around loops and response sink**

```go
type Session struct {
	ID        uint64
	conn      Conn
	dispatcher Dispatcher
	logger    log.Logger
	provider  mempool.Provider
	responses chan respkit.ScopedResponse
	maxInFlight int
	inflight atomic.Int32
	stopCh   chan struct{}
	doneCh   chan struct{}
}
```

Add a `HandleResponse(respkit.ScopedResponse)` method so dispatcher can push responses back into the session.

- [ ] **Step 4: Implement `readLoop` with per-request scope creation and inflight gating**

```go
func (s *Session) readLoop() {
	for {
		if s.shouldStopReading() {
			return
		}
		s.waitForInflightSlot()
		scope := s.provider.NewScope()
		request, err := s.conn.Read(scope)
		if err != nil {
			scope.Release()
			s.stopWithError(err)
			return
		}
		s.inflight.Add(1)
		if err := s.dispatcher.Submit(respkit.ScopedRequest{Session: s, Request: request, Scope: scope}); err != nil {
			s.inflight.Add(-1)
			scope.Release()
			s.stopWithError(err)
			return
		}
	}
}
```

- [ ] **Step 5: Implement `writeLoop` with flush-then-release semantics**

```go
func (s *Session) writeLoop() {
	batch := make([]respkit.ScopedResponse, 0, 8)
	for {
		resp, ok := <-s.responses
		if !ok {
			return
		}
		batch = append(batch[:0], resp)
		if err := s.conn.Write(resp.Response); err != nil {
			s.releaseBatch(batch)
			s.stopWithError(err)
			return
		}
		for len(s.responses) > 0 {
			next := <-s.responses
			batch = append(batch, next)
			if err := s.conn.Write(next.Response); err != nil {
				s.releaseBatch(batch)
				s.stopWithError(err)
				return
			}
		}
		if err := s.conn.Flush(); err != nil {
			s.releaseBatch(batch)
			s.stopWithError(err)
			return
		}
		s.releaseBatch(batch)
	}
}
```

- [ ] **Step 6: Run session tests**

Run: `go test ./internal/session -run TestSession`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go
git commit -m "refactor: rebuild session around loops and scope lifecycle"
```

### Task 6: Rebuild server lifecycle around dispatcher and new sessions

**Files:**
- Modify: `server.go`
- Modify: `server_test.go`
- Test: `go test ./... -run 'TestServer(Start|Stop|Close)'`

- [ ] **Step 1: Write the failing lifecycle test for accept/session registration**

```go
func TestServerStartRegistersSessionAndStopsGracefully(t *testing.T) {
	cfg := &Config{Addr: "127.0.0.1:0", DispatcherWorkers: 1, QueueSize: 8}
	srv := NewServer(cfg)

	done := make(chan error, 1)
	go func() { done <- srv.Start() }()
	waitFor(func() bool { return srv.Addr() != nil })

	conn, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	waitFor(func() bool { return srv.ActiveSessions() == 1 })
	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Start() returned error = %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestServerStartRegistersSessionAndStopsGracefully`
Expected: FAIL because server still depends on the old handler/factory path.

- [ ] **Step 3: Rewrite `Server` to own listener, dispatcher, and sessions only**

```go
type Server struct {
	config     *Config
	dispatcher *dispatcher.Dispatcher
	listener   net.Listener
	sessions   sync.Map
	nextSessID atomic.Uint64
	done       atomic.Bool
	serveDone  chan struct{}
	logger     log.Logger
}
```

Remove all `handler` and `factory` fields.

- [ ] **Step 4: Rebuild `Start()` to initialize dispatcher before entering accept loop**

```go
func (s *Server) Start() error {
	ln, err := net.Listen(s.config.Network, s.config.Addr)
	if err != nil {
		return err
	}
	s.listener = ln
	s.serveDone = make(chan struct{})
	s.dispatcher = dispatcher.New(s.config.DispatcherWorkers, s.config.QueueSize, s.config.Logger)
	s.dispatcher.Start()
	defer close(s.serveDone)
	defer s.dispatcher.StopGracefully()
	return s.serve()
}
```

- [ ] **Step 5: Rebuild the accept path to create the new conn/session stack**

```go
func (s *Server) serve() error {
	for {
		netConn, err := s.listener.Accept()
		if err != nil {
			if s.done.Load() || errors.Is(err, net.ErrClosed) {
				return nil
			}
			s.logger.Error("accept failed", "error", err)
			continue
		}
		id := s.nextSessID.Add(1)
		conn := conn.NewConn(netConn, protocol.NewReader(netConn), protocol.NewWriter(netConn))
		sess := session.NewSession(id, conn, s.dispatcher, s.config.MemPool, s.config.Logger, s.config.MaxInFlightPerSession)
		s.sessions.Store(id, sess)
		sess.SetOnClose(func() { s.sessions.Delete(id) })
		sess.Start()
	}
}
```

- [ ] **Step 6: Implement `Stop()` and `Close()` against the new lifecycle**

```go
func (s *Server) Stop() error {
	s.done.Store(true)
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.sessions.Range(func(_, value any) bool {
		value.(*session.Session).StopGracefully()
		return true
	})
	<-s.serveDone
	return nil
}

func (s *Server) Close() error {
	s.done.Store(true)
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.sessions.Range(func(_, value any) bool {
		_ = value.(*session.Session).CloseNow()
		return true
	})
	<-s.serveDone
	return nil
}
```

- [ ] **Step 7: Run server lifecycle tests**

Run: `go test ./... -run 'TestServer(Start|Stop|Close)'`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add server.go server_test.go respkit.go
git commit -m "refactor: rebuild server lifecycle around dispatcher"
```

### Task 7: Delete mux/handler/factory path and update example

**Files:**
- Delete: `mux.go`
- Modify: `example/basic-server.go`
- Modify: `example/basic_server_test.go`
- Test: `go test ./example ./...`

- [ ] **Step 1: Write the failing example test for the new server API**

```go
func TestBasicServerRespondsToPing(t *testing.T) {
	srv := respkit.NewServer(&respkit.Config{Addr: "127.0.0.1:0"})
	done := make(chan error, 1)
	go func() { done <- srv.Start() }()
	defer func() {
		_ = srv.Stop()
		<-done
	}()

	waitFor(func() bool { return srv.Addr() != nil })
	conn, err := net.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("*1\r\n$4\r\nPING\r\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got := string(buf[:n]); got != "+PONG\r\n" {
		t.Fatalf("response = %q, want +PONG\\r\\n", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./example ./... -run TestBasicServerRespondsToPing`
Expected: FAIL because the example still uses the old mux/handler API.

- [ ] **Step 3: Delete `mux.go` and remove any remaining handler/factory references**

Use file deletion for `mux.go`, then remove old imports/usages from `server.go`, `respkit.go`, and tests.

- [ ] **Step 4: Rewrite the example server around the new API**

```go
func main() {
	srv := respkit.NewServer(&respkit.Config{Addr: ":6380"})
	if err := srv.Start(); err != nil {
		log.Fatal(err)
	}
}
```

The example should demonstrate the built-in `PING`/`ECHO` command path, not handler registration.

- [ ] **Step 5: Run example and full package tests**

Run: `go test ./example ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add -u example/basic-server.go example/basic_server_test.go mux.go server.go respkit.go
git commit -m "refactor: remove mux handler factory path"
```

### Task 8: Full verification, race testing, and cleanup fixes

**Files:**
- Modify: any touched file needed to fix failing verification
- Test: `go test ./...`, `go test -race ./...`

- [ ] **Step 1: Run the full test suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 2: Run the race detector**

Run: `go test -race ./...`
Expected: PASS

- [ ] **Step 3: If tests fail, apply minimal fixes in place**

Typical fix shape:

```go
if err := s.conn.Flush(); err != nil {
	s.releaseBatch(batch)
	s.stopWithError(err)
	return
}
```

Do not add compatibility layers; only fix correctness issues in the new architecture.

- [ ] **Step 4: Re-run the exact failing command after each fix**

Examples:

```bash
go test ./internal/session -run TestSessionReleasesScopeAfterFlush
go test ./internal/dispatcher -run TestDispatcher
go test ./...
go test -race ./...
```

Expected: PASS after each targeted fix.

- [ ] **Step 5: Commit final verification fixes**

```bash
git add server.go respkit.go internal/conn/conn.go internal/session/session.go internal/dispatcher/dispatcher.go internal/command/command.go example/basic-server.go server_test.go
git commit -m "test: verify redesigned server runtime"
```

---

## Self-Review

### Spec coverage
- Server-centered lifecycle: covered by Tasks 1 and 6.
- Session read/write loops: covered by Task 5.
- Global dispatcher with scheduler and state queues: covered by Task 4.
- `Conn` as pure I/O boundary: covered by Task 2.
- `protocol.RespValue` as protocol object: covered by Tasks 2 and 3.
- `command` module: covered by Task 3.
- `basekit` mempool `Scope` usage across request lifecycle: covered by Tasks 2, 5, and 8.
- `basekit` log module usage: covered by Tasks 1, 4, 5, and 6.
- Delete `mux.go` / `Handler` / `CommandFactory`: covered by Tasks 1 and 7.
- Tests and race verification: covered by Task 8.

### Placeholder scan
- No `TBD`, `TODO`, or “implement later” placeholders remain.
- Each coding task includes concrete code blocks and exact commands.
- Each testing step includes a specific command and expected result.

### Type consistency
- Public flow uses `ScopedRequest` / `ScopedResponse` consistently.
- Request scope type is consistently `*mempool.Scope`.
- Logging is consistently described as `basekit` log module via `log.Logger`.
- Dispatcher flow consistently uses `incomingQueue`, `readyQueue`, and `completeQueue`.

### Known implementation risk to watch
- Because `basekit` is a local module path, the exact import names (`log`, `mempool`, `Provider`, `Scope`, `NewNopLogger`, `NewProvider`) must be validated against the actual package API before coding. If names differ, adapt the code while preserving the architecture and lifecycle defined here.
