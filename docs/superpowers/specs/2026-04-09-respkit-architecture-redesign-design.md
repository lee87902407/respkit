# Respkit 新架构重构设计

- 日期：2026-04-09
- 主题：以 Server 为中心、Session 双协程、全局 Dispatcher、Scope 驱动内存生命周期的破坏性重构
- 状态：已确认设计，待实现计划

## 1. 背景与目标

当前实现虽然已经恢复了部分 `Server` / `Handler` / `Mux` 风格 API，但整体主链路仍然是单连接单循环：读、解析、处理、写回都在同一条处理路径内完成。它与目标架构存在明显偏差：

- `Server` 虽然是生命周期入口，但内部仍偏向同步 `serve()` 循环
- `Session` 尚未成为连接级并发协调核心
- `Conn` 虽已变薄，但还不是严格意义上的纯网络 I/O 边界
- 没有独立的 `dispatcher` 模块
- 没有读写双协程模型
- 没有 `basekit.Scope` 驱动的请求级内存生命周期模型
- 旧的 `Mux` / `Handler` / `CommandFactory` 抽象仍在主路径上

本次重构目标是直接切换到新架构，不做兼容桥接，不保留旧主路径。

## 2. 设计目标

1. 程序架构以 `Server` 为中心，通过 `Start` / `Stop` / `Close` 控制生命周期。
2. `Server` 启动后负责监听、accept 新连接、创建 `Session`、维护 session map、启动和停止全局 `Dispatcher`。
3. 每个连接对应一个 `Session`，每个 `Session` 内固定两条协程：`readLoop` 和 `writeLoop`。
4. `Dispatcher` 为全局共享模块，采用“单独一个调度线程 + 多个状态队列”的结构。
5. `Conn` 保留，但只负责纯网络 I/O，不再承载业务写接口或命令分发逻辑。
6. `protocol` 模块负责 `RespValue` 及 RESP 协议编解码。
7. `command` 模块负责命令对象构建和业务执行。
8. 每个请求必须创建一个 `basekit` mempool 模块提供的 `Scope`，且请求处理全链路涉及的请求期内存申请都必须统一走该 `Scope`。
9. 请求处理过程中的日志统一使用 `basekit` 的 `log` 模块。
10. `Scope` 的生命周期延续到对应响应完成写回并 flush 后才能释放。
11. 旧的 `mux.go` / `Handler` / `CommandFactory` 直接删除。

## 3. 非目标

以下内容不在本次第一阶段范围内：

1. 保留旧 API 兼容层。
2. 保留 `Mux` / `Handler` / `CommandFactory` 的桥接路径。
3. 完整资源感知调度策略的实现。
4. 在第一阶段引入复杂的资源抢占、等待唤醒、资源依赖图等高级调度策略。

注意：虽然完整资源感知调度不在第一阶段范围内，但 `Dispatcher` 的骨架必须按“调度线程 + 多状态队列”设计，以便后续扩展。

## 4. 总体架构

新的主链路为：

`Server`
→ accept `net.Conn`
→ 创建精简 `Conn`
→ 创建 `Session`
→ `Session.readLoop` 创建请求级 `Scope` 并调用 `Conn.Read(scope, ...)`
→ `protocol` 解析为 `RespValue`
→ 异步投递到全局 `Dispatcher`
→ `Dispatcher` 将 `RespValue` 转为 `command.Command`
→ 执行命令，生成响应 `RespValue`
→ 回投到 `Session.writeLoop`
→ `Session.writeLoop` 写回客户端并在队列清空时 flush
→ flush 成功后释放该请求对应的 `Scope`

关键原则：

- `Server` 是唯一生命周期中心。
- `Session` 是连接级并发协调中心。
- `Dispatcher` 是全局共享调度中心。
- `Conn` 是纯网络边界。
- `protocol` 管协议，`command` 管业务。
- `Scope` 是单请求内存边界。

## 5. 模块职责

### 5.1 Server

负责：

- 启动 listener
- 运行 accept 循环
- 创建 `Conn` 与 `Session`
- 将 `Session` 注册到 session map
- 启动和停止全局 `Dispatcher`
- 提供 `Start` / `Stop` / `Close`
- 等待所有 session 和 dispatcher 退出

不负责：

- RESP 解析细节
- 命令执行细节
- 单连接读写队列管理

### 5.2 Session

负责：

- 管理单连接状态
- 维护 `readLoop` 和 `writeLoop`
- 管理单连接 inflight 请求数量
- 管理响应队列
- 管理请求级 `Scope` 的最终释放时机
- 协调优雅停止与强制关闭

不负责：

- 全局调度策略
- 命令执行实现
- listener 生命周期

### 5.3 Conn

负责：

- 对底层 `net.Conn` 做网络 I/O 封装
- 基于外部传入的 `Scope` 读取一个请求
- 写出 `protocol.RespValue`
- flush、deadline、close、remote addr 等网络控制

不负责：

- 请求期内存拥有权
- Session 生命周期
- 命令分发
- 业务写接口

### 5.4 protocol

负责：

- 定义 `RespValue`
- RESP 协议解析
- RESP 协议编码
- 命令名和参数提取等协议辅助能力

### 5.5 command

负责：

- 定义命令对象接口
- 将请求 `RespValue` 转换为 `Command`
- 执行业务逻辑并返回响应 `RespValue`

### 5.6 Dispatcher

负责：

- 接收 `Session` 投递的请求
- 调度线程对请求进行状态归类
- 将可执行请求放入 ready 队列
- 通知 worker 执行命令
- 接收 worker 完成结果并回投给对应 `Session`
- 使用 `basekit` 的 `log` 模块记录调度、错误和生命周期日志

### 5.7 Scope

负责：

- 使用 `basekit` mempool 模块提供的 `Scope` 作为单请求生命周期内的内存与临时对象归属
- 提供 `Conn.Read`、`protocol` 解析、`command` 执行过程中用到的请求期内存
- 在响应写回并 flush 完成后统一释放

## 6. 并发模型

### 6.1 Session 双协程模型

每个 `Session` 固定包含：

- `readLoop`
- `writeLoop`

#### readLoop

流程：

1. 创建一个新的 `basekit.Scope`
2. 使用 `Scope` 提供请求期内存与 buffer
3. 调用 `Conn.Read(scope, ...)` 读取并解析一个请求，得到 `RespValue`
4. 若当前 inflight 数已达到 `MaxInFlightPerSession`，阻塞等待
5. 将 `ScopedRequest` 异步投递到 `Dispatcher`
6. inflight 加一
7. 继续读取下一条请求

#### writeLoop

流程：

1. 从响应队列取出 `ScopedResponse`
2. 调用 `Conn.Write(resp)` 写入连接缓冲
3. 当响应队列仍非空时继续写，不立即 flush
4. 当响应队列为空时执行一次 `Flush()`
5. flush 成功后释放这一批已写出的响应所绑定的 `Scope`
6. inflight 对应递减

该模型满足：

- 读写解耦
- dispatcher 异步处理
- 写线程负责“写到空再 flush”
- Scope 在真正写回完成前不会被释放

### 6.2 单 Session 背压机制

通过配置项 `MaxInFlightPerSession` 控制每个 session 允许投递到 dispatcher 但尚未完成的请求数。

规则：

- 当 `inflight < MaxInFlightPerSession` 时，`readLoop` 可继续读取并投递新请求
- 当 `inflight >= MaxInFlightPerSession` 时，`readLoop` 阻塞等待
- 当对应响应被 flush 并确认完成后，`inflight` 递减
- 递减后，`readLoop` 恢复投递

该机制保证：

- 不会无限读取导致内存堆积
- 背压发生在连接侧，符合调用方对投递数量限制的要求

## 7. Dispatcher 设计

### 7.1 总体结构

`Dispatcher` 为全局共享组件，采用：

- 一个 scheduler goroutine
- 多个状态队列
- 多个 worker goroutine

第一阶段至少包含：

- `incomingQueue`：接收 session 新投递请求
- `readyQueue`：可立即执行的请求队列
- `completeQueue`：worker 完成后的结果回传队列
- `waitingQueue`：预留给后续资源感知调度的等待队列

### 7.2 第一阶段行为

调度线程流程：

1. 从 `incomingQueue` 接收 `ScopedRequest`
2. 判断是否可执行
3. 第一阶段默认所有请求都可执行
4. 可执行请求进入 `readyQueue`
5. worker 从 `readyQueue` 取任务执行
6. 执行完成后将 `ScopedResponse` 发送到 `completeQueue`
7. scheduler 再将结果回投给对应的 session 响应队列

### 7.3 未来扩展点

后续若启用资源感知调度：

- scheduler 可将暂不可执行请求放入 `waitingQueue`
- 当收到资源释放事件后，scheduler 重新检查 `waitingQueue`
- 满足条件的请求再进入 `readyQueue`

因此第一版虽然不实现完整资源感知逻辑，但 dispatcher 骨架必须按此方式搭建。

## 8. Scope 生命周期设计

### 8.1 Scope 生命周期边界

每个请求一个 `Scope`，生命周期覆盖：

`read`
→ `parse`
→ `dispatch`
→ `command execute`
→ `response enqueue`
→ `write`
→ `flush`
→ `release`

### 8.2 关键约束

1. `Conn.Read()` 必须强依赖外部 `Scope`。
2. 请求期所有内存分配都必须通过 `Scope`。
3. `RespValue` 允许引用 `Scope` 管理的内存。
4. dispatcher 不拥有 `Scope`，只透传它。
5. 最终由 `Session.writeLoop` 在写回完成后释放 `Scope`。

### 8.3 为什么不能提前释放

因为请求参数、命令执行中间结果、响应内容都可能引用 `Scope` 内存。如果在 dispatcher 执行结束时就释放 `Scope`，则写回阶段会访问悬垂引用，导致内存错误。

因此唯一正确的释放点是：

- 对应响应已经写入
- 对应批次 flush 成功
- 响应不再需要继续访问 `Scope` 中的内存

## 9. 核心对象草图

以下是逻辑草图，不要求与最终代码命名完全一致，但职责必须保持一致。

### 9.1 Config

建议包含：

- `Addr`
- `Network`
- `ReadBufferSize`
- `WriteBufferSize`
- `DispatcherWorkers`
- `QueueSize`
- `MaxInFlightPerSession`
- `MemPool`
- `IdleTimeout`
- `Logger`

说明：

- `QueueSize` 用于 dispatcher 队列容量
- `DispatcherWorkers` 用于 worker 数量
- `MaxInFlightPerSession` 用于单连接背压控制
- `MemPool` 为 `basekit` mempool / Scope 管理接入点
- `Logger` 为 `basekit` log 模块接入点

### 9.2 ScopedRequest

包含：

- `Session`
- `Request protocol.RespValue`
- `Scope *basekit.Scope`

### 9.3 ScopedResponse

包含：

- `Session`
- `Response protocol.RespValue`
- `Scope *basekit.Scope`

### 9.4 Command

建议统一接口：

- `Execute(*ExecContext) (protocol.RespValue, error)`

并配套一个构建入口：

- `BuildCommand(request protocol.RespValue) (Command, error)`

第一阶段至少实现：

- `PING`
- `ECHO`
- 未知命令返回标准错误

## 10. 生命周期语义

### 10.1 Start

`Server.Start()`：

- 启动 listener
- 启动 dispatcher
- 阻塞运行 accept loop
- 直到 server 停止、所有 session 退出、dispatcher 退出后返回

### 10.2 Stop

`Server.Stop()` 为优雅停止：

1. 停止 accept 新连接
2. 通知所有 session 停止读取新请求
3. 保留已投递请求的处理与写回
4. 等待所有 session drain 完成
5. 停止 dispatcher
6. `Start()` 返回

### 10.3 Close

`Server.Close()` 为强制关闭：

1. 停止 accept
2. 直接关闭所有 session 底层连接
3. 允许未完成请求被中断
4. dispatcher 尽快退出
5. `Start()` 返回

### 10.4 Session 退出

#### 优雅退出

- `readLoop` 不再读取新请求
- 等待 inflight 归零
- 等待 response 队列清空
- 完成 flush
- 释放剩余 Scope
- session 从 server map 删除

#### 强制退出

- 关闭底层连接
- `readLoop` / `writeLoop` 因 I/O 错误退出
- 未完成请求对应的 Scope 仍需在退出路径中释放

## 11. 错误处理约定

### 11.1 返回给客户端的错误

以下错误应转换成 RESP error response：

- 协议解析错误
- `RespValue -> Command` 构建错误
- 命令执行错误
- 未知命令错误

### 11.2 直接终止 Session 的错误

以下错误应终止当前 session：

- 底层连接读失败
- 底层连接写失败
- flush 失败
- session 正在强制关闭

原则：

- 业务/协议错误 → 返回响应
- I/O 错误 → 终止连接

## 12. API 变化与删除策略

本次是破坏性重构，不保留兼容层。

### 12.1 直接删除

- `mux.go`
- `Handler`
- `HandlerFunc`
- `CommandFactory`
- 旧的 handler-based dispatch 主路径
- 旧的 `Conn.WriteString/WriteBulk/WriteInt/WriteArray/WriteNull/WriteAny` 等接口

### 12.2 重建公开 API

新的公开 API 应围绕：

- `Server`
- `Config`
- `Session`
- `Dispatcher`
- `protocol.RespValue`
- `command.Command`

来重新组织。

## 13. 文件迁移计划

### 13.1 重写

- `server.go`
- `respkit.go`
- `internal/session/session.go`
- `internal/conn/conn.go`

### 13.2 保留并扩展

- `internal/protocol/types.go`

### 13.3 新增

- `internal/dispatcher/`
- `internal/command/`

### 13.4 删除

- `mux.go`
- 所有围绕 `Handler` / `CommandFactory` 的旧路径与测试

## 14. 实施顺序

建议实现顺序如下：

1. 引入 `basekit` 依赖与 `Scope` 接入点
2. 设计并实现新的 `protocol` / `Conn.Read(scope, ...)` 读链路
3. 引入 `dispatcher` 骨架：scheduler + 多状态队列 + workers
4. 重写 `Session` 为双协程模型
5. 实现 `ScopedRequest` / `ScopedResponse` 与 Scope 生命周期闭环
6. 引入 `command` 模块并实现基础命令
7. 重写 `Server` 接入新主链路
8. 删除 `mux.go` / `Handler` / `CommandFactory`
9. 重建测试
10. 跑完整测试与 race 检查

## 15. 测试策略

### 15.1 Server 生命周期测试

覆盖：

- `Start()` 阻塞运行
- `Stop()` 优雅停止
- `Close()` 强制关闭
- accept 新连接后 session 正确注册
- 停止后不再 accept 新连接
- session 与 dispatcher 退出后 `Start()` 返回

### 15.2 Session 并发与背压测试

覆盖：

- `readLoop` / `writeLoop` 并发工作
- 多请求异步投递
- `MaxInFlightPerSession` 达上限时读阻塞
- 响应 flush 后 inflight 正确递减
- 写失败时 session 正确退出

### 15.3 Dispatcher 调度测试

覆盖：

- `incomingQueue -> scheduler -> readyQueue -> worker`
- 单调度线程行为正确
- worker 完成后结果能回投 session
- stop/close 时 scheduler 与 worker 正确退出
- `waitingQueue` 作为未来扩展点不影响第一版行为

### 15.4 Scope 生命周期测试

必须覆盖：

- 每请求一个 `Scope`
- `Conn.Read()` 使用外部 `Scope`
- parse / execute / response 使用 Scope 内存
- response flush 成功后才释放 `Scope`
- 写失败或强制关闭时也能释放 `Scope`
- 不出现提前释放或泄漏

### 15.5 Protocol / Command 测试

覆盖：

- `RespValue` 编解码
- 命令名/参数提取
- `RespValue -> Command`
- `PING` / `ECHO`
- 未知命令行为

### 15.6 验证命令

实现阶段完成后至少执行：

- `go test ./...`
- `go test -race ./...`
- 必要时 `go test -cover ./...`

## 16. 风险与注意事项

1. 这是主架构替换，diff 会较大。
2. 删除旧抽象后，example 与测试需要同步重写。
3. Scope 生命周期设计错误会直接导致悬垂引用或泄漏，必须重点验证。
4. scheduler 单线程是架构约束，后续不要在 worker 内偷偷承担调度职责。
5. 第一阶段不做完整资源感知调度，但状态队列骨架必须一次设计正确。

## 17. 最终决策摘要

已确认的核心决策：

1. 优先新架构，不做兼容桥接。
2. `Server` 作为唯一生命周期中心。
3. `Session` 采用读写双协程。
4. `Dispatcher` 为全局共享组件。
5. `Dispatcher` 采用“单独一个调度线程 + 多个状态队列”。
6. `Conn` 保留但只做纯网络 I/O。
7. `protocol` 以 `RespValue` 为统一协议对象。
8. `command` 模块负责业务命令对象。
9. 必须直接引入 `basekit.Scope`。
10. `Conn.Read()` 强依赖外部 `Scope` 生命周期。
11. 请求期所有内存申请统一走 `Scope`。
12. `Scope` 在响应写回并 flush 成功后释放。
13. `mux.go` / `Handler` / `CommandFactory` 直接删除。

---

本设计文档用于后续实现计划编写与代码重构执行，默认实现阶段以本文件为唯一架构基线，除非用户再次明确调整设计。
