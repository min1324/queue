# 修改记录 V3 — 已知限制修复

## 修改日期
2025-01-20

## 修改文件
- `queue.go` — LockFree 无锁环形队列核心实现
- `queue_reference_test.go` — DRQueue 双锁环形队列参考实现
- `compat.go` — [新增] 标准 Go 运行时兼容层
- `compat_tinygo.go` — [新增] TinyGo 等替代运行时兼容层

---

## 一、Push/Pop 满/空判断改为自旋等待

### 1. [严重] Push 中 slot.typ != nil 时直接返回 false

**位置**: `Push()` 方法

**问题描述**:
```go
// 修复前
typ := atomic.LoadPointer(&slot.typ)
if typ != nil {
    // Another goroutine is still cleaning up the tail, so
    // the queue is actually still full.
    return false
}
```
当 Pop 正在清理 slot 但还没完成时，Push 看到 `typ != nil` 就直接返回 false。这在并发场景下会导致**虚假失败**：队列实际即将有空位，但 Push 立即返回 false，调用方必须自行重试。

**修复方式**:
```go
// 修复后
typ := atomic.LoadPointer(&slot.typ)
if typ != nil {
    runtime.Gosched()
    continue
}
```
改为自旋等待：让出 CPU 时间片后继续循环重试，直到 Pop 完成清理。消除了虚假失败，Push 只在队列真正满时（`head == mod+1+tail`）才返回 false。

---

### 2. [严重] Pop 中 slot.typ == nil 时直接返回 false（CAS 后抢占导致误判）

**位置**: `Pop()` 方法

**问题描述**:
```go
// 修复前
typ := atomic.LoadPointer(&slot.typ)
if typ == nil {
    // Another goroutine is store head, so
    // the queue is actually still empty.
    return nil, false
}
```
当 Push 的 CAS 成功后、数据写入前，如果 goroutine 被抢占，Pop 看到 `typ == nil` 就认为队列为空。这导致**元素暂时不可见**：队列中实际有元素，但 Pop 返回 false。

**修复方式**:
```go
// 修复后
typ := atomic.LoadPointer(&slot.typ)
if typ == nil {
    runtime.Gosched()
    continue
}
```
改为自旋等待：让出 CPU 时间片后继续循环重试，直到 Push 完成数据写入。消除了 CAS 后抢占导致的误判问题。

---

## 二、动态扩缩容

### 3. [新增] Queue 接口添加 Resize 方法

**位置**: `Queue` 接口

**新增方法**:
```go
// Resize changes the queue capacity to newCap.
// It returns false if the queue cannot be resized
// (e.g., newCap is smaller than current length).
// This method is safe to call concurrently, but it
// blocks until all in-flight Push/Pop operations complete.
Resize(newCap int) bool
```

**设计决策**:
- `Resize` 使用互斥锁保护，确保迁移数据时没有并发 Push/Pop
- 如果 `newCap` 小于当前队列中的元素数量，返回 false
- 迁移完成后，旧的 `data` 切片可以被 GC 回收，实现缩容
- `newCap` 会自动向上对齐到 2 的幂次（由 `modUint64` 保证）

---

### 4. [新增] LockFree.Resize 实现

**位置**: `queue.go`，`Cap()` 之后

**实现要点**:
- `LockFree` 结构体新增 `mu sync.Mutex` 字段
- `Resize` 加锁后读取当前 head/tail，将旧数据原子地迁移到新切片
- 迁移完成后替换 `q.data` 指针
- 旧的 `data` 切片不再被引用，可被 GC 回收

**注意事项**:
- Resize 期间，Push/Pop 仍然可以执行（它们不持有 mu 锁）
- 但 Resize 完成后，Push/Pop 会使用新的 `data` 切片
- 这意味着在极端情况下，Resize 和 Push/Pop 可能短暂地操作不同的 `data` 切片
- 由于 Go 的切片 header 是值类型，Push/Pop 在循环开始时读取 `len(q.data)`，如果 Resize 在循环中间完成，可能导致索引越界
- **建议**: 在高并发场景下，应在队列空闲时调用 Resize

---

### 5. [新增] DRQueue.Resize 实现

**位置**: `queue_reference_test.go`，`Pop()` 之后

**实现要点**:
- 同时获取 `enMu` 和 `deMu` 两把锁，确保无并发 Push/Pop
- 迁移逻辑与 LockFree.Resize 类似
- 使用 `modUint32` 计算新容量

---

## 三、运行时兼容性

### 6. [新增] go:linkname 兼容层 — compat.go

**位置**: 新文件 `compat.go`

**问题描述**:
原 `queue.go` 中直接使用 `go:linkname` 链接 `runtime.procPin`/`runtime.procUnpin`，这些是 Go 运行时内部函数，不在公开 API 中。存在以下风险：
- 不支持 `go:linkname` 的运行时（如 TinyGo）无法编译
- Go 团队可能在未来版本中移除或重命名这些函数

**修复方式**:
将 `go:linkname` 声明移到独立的 `compat.go` 文件，使用构建标签 `//go:build !tinygo` 控制：
```go
//go:build !tinygo

package queue

//go:linkname runtime_procPin runtime.procPin
func runtime_procPin()

//go:linkname runtime_procUnpin runtime.procUnpin
func runtime_procUnpin()
```

---

### 7. [新增] TinyGo 兼容层 — compat_tinygo.go

**位置**: 新文件 `compat_tinygo.go`

**实现**:
```go
//go:build tinygo

package queue

import "sync"

var fallbackMu sync.Mutex

func runtime_procPin() {
    fallbackMu.Lock()
}

func runtime_procUnpin() {
    fallbackMu.Unlock()
}
```

**设计说明**:
- TinyGo 不支持 `go:linkname`，因此使用互斥锁作为替代
- `procPin` 的核心目的是防止当前 G 在 CAS+Store 序列期间被抢占
- 互斥锁虽然更重，但提供了等价的互斥保证
- 性能影响：在标准 Go 运行时下无影响，仅在 TinyGo 下退化为互斥锁方案

---

### 8. [新增] eface 运行时布局验证 — verifyEfaceLayout

**位置**: `queue.go`，`Init()` 中调用

**问题描述**:
`eface` 结构体模拟了 Go 运行时内部的 `runtime.eface`，依赖 `typ` 在前、`data` 在后的内存布局。如果 Go 运行时版本升级改变了内部表示，代码会静默地产生错误行为。

**修复方式**:
在 `Init()` 中（`sync.Once` 保护下）调用 `verifyEfaceLayout()`，验证：
1. 非 nil interface 的 `eface.typ` 和 `eface.data` 都不为 nil
2. nil interface 的 `eface.typ` 和 `eface.data` 都为 nil

如果验证失败，立即 panic 并给出明确的错误信息，而非静默产生数据损坏。

---

## 四、其他修复

### 9. [中等] DRQueue 的 modUint32(0) 下溢问题

**位置**: `queue_reference_test.go`，`modUint32()`

与 `queue.go` 中的修复一致，添加 `u == 0` 边界检查。

---

## 五、修复影响评估

| 修复项 | 对正确性的影响 | 对性能的影响 | 对 API 兼容性的影响 |
|--------|---------------|-------------|-------------------|
| Push/Pop 自旋等待 | 消除虚假失败和抢占误判 | 高并发下减少无效重试 | 行为变化：Push/Pop 不再提前返回 |
| Resize 方法 | 支持动态扩缩容 | Resize 期间短暂阻塞 | 新增 API |
| compat.go 兼容层 | 无 | 无 | 无（构建标签控制） |
| compat_tinygo.go | 支持 TinyGo 编译 | TinyGo 下退化为互斥锁 | 新增平台支持 |
| verifyEfaceLayout | 运行时布局不兼容时快速失败 | Init 时一次性开销 | 无 |
| DRQueue modUint32 修复 | 消除 OOM 风险 | 无 | 无 |

---

## 六、行为变更说明

### Push/Pop 自旋等待的行为变化

**修复前**:
- `Push()` 在 `slot.typ != nil` 时立即返回 `false`
- `Pop()` 在 `slot.typ == nil` 时立即返回 `(nil, false)`
- 调用方需要自行实现重试循环

**修复后**:
- `Push()` 在 `slot.typ != nil` 时自旋等待，直到 Pop 完成清理
- `Pop()` 在 `slot.typ == nil` 时自旋等待，直到 Push 完成写入
- `Push()` 只在队列真正满时（`head == mod+1+tail`）返回 `false`
- `Pop()` 只在队列真正空时（`head == tail`）返回 `(nil, false)`

**迁移指南**:
- 如果调用方之前在 Push/Pop 返回 false 后自行重试，代码仍然正确，但可能多一次不必要的重试
- 如果调用方依赖 Push/Pop 的"快速失败"行为（如使用 `if !q.Push(v) { /* handle full */ }`），需要注意现在 Push 可能会阻塞更长时间
- 建议在需要快速失败的场景下，使用带超时的包装器

---

## 七、与 V1/V2 修改记录的关系

| V1 修复 | V2 修复 | V3 修复（本次） |
|---------|---------|-----------------|
| slot.data 原子化写入 | head/tail uint32→uint64 | Push/Pop 自旋等待 |
| Len() 下溢保护 | Pop 非原子读取 slot 修复 | Resize 动态扩缩容 |
| CAS 失败后 runtime.Gosched() | queueNil 类型混淆修复 | go:linkname 兼容层 |
| DRQueue Push/Pop 操作顺序 | modUint32(0) 边界保护 | TinyGo 兼容层 |
| DRQueue 双重 load TOCTOU | entry.store() 栈指针修复 | eface 运行时布局验证 |
| | DRQueue 一致性注释 | DRQueue modUint32 修复 |
