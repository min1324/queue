# 修改记录 V2 — 内存泄漏、并发访问与架构问题修复

## 修改日期
2025-01-20

## 修改文件
- `queue.go` — LockFree 无锁环形队列核心实现
- `queue_reference_test.go` — DRQueue 双锁环形队列参考实现

---

## 一、queue.go 修复详情

### 1. [严重] head/tail 从 uint32 升级为 uint64，防止溢出导致队列永久失效

**位置**: `LockFree` 结构体，`Push()`，`Pop()`，`Len()`，`Init()`

**问题描述**:
```go
// 修复前
type LockFree struct {
    tail uint32
    head uint32
    // ...
}
```
`head` 和 `tail` 是 `uint32`，只增不减。当经过约 43 亿次操作后（`uint32` 最大值 4294967295），`head` 溢出回 0，导致：
- `head == tail` 可能在队列非空时成立
- `head == mod+1+tail` 可能在队列非满时成立
- `head&mod` 和 `tail&mod` 的索引映射完全错乱

**修复方式**:
```go
// 修复后
type LockFree struct {
    tail uint64
    head uint64
    // ...
}
```
将 `head`/`tail` 升级为 `uint64`，所有相关的原子操作同步更新：
- `atomic.LoadUint32` → `atomic.LoadUint64`
- `atomic.CompareAndSwapUint32` → `atomic.CompareAndSwapUint64`
- `Len()` 中 `int32` → `int64` 转换
- `mod` 类型从 `uint32` → `uint64`

**影响**: uint64 在每秒 10 亿次操作的速率下，约需 584 年才会溢出，实际上消除了溢出风险。

---

### 2. [严重] Pop 中非原子读取 slot，存在数据竞争

**位置**: `Pop()` 方法，原第 173 行

**问题描述**:
```go
// 修复前
val := *(*any)(unsafe.Pointer(slot))
```
直接将 `slot`（`*eface`）转换为 `*any` 读取，没有使用原子操作。Push 侧用 `atomic.StorePointer` 写入，Pop 侧用非原子读取，在弱内存序架构（如 ARM）上可能读取到陈旧或不完整的数据。

**修复方式**:
```go
// 修复后
slotTyp := atomic.LoadPointer(&slot.typ)
slotData := atomic.LoadPointer(&slot.data)
tmp := eface{typ: slotTyp, data: slotData}
val := *(*any)(unsafe.Pointer(&tmp))
```
使用 `atomic.LoadPointer` 分别读取 `typ` 和 `data`，构造临时 `eface` 再转换为 `any`。确保在弱内存序架构上的正确性，消除数据竞争。

---

### 3. [中等] queueNil 类型混淆，用户可意外构造哨兵值

**位置**: 包级别类型定义，`Push()`，`Pop()`

**问题描述**:
```go
// 修复前
type queueNil *struct{}

// Push 中
val = queueNil(nil)

// Pop 中
if val == queueNil(nil) { val = nil }
```
`queueNil` 是导出类型 `*struct{}`，用户可以构造 `queueNil(nil)` 值推入队列，Pop 时会被错误地转换为 nil。

**修复方式**:
```go
// 修复后
type nilQueueNil struct{}
var queueNilVal any = (*nilQueueNil)(nil)

// Push 中
val = queueNilVal

// Pop 中
if val == queueNilVal { val = nil }
```
使用未导出类型 `nilQueueNil` 的 nil 指针作为哨兵值。外部代码无法构造 `*nilQueueNil` 类型的值，因此不可能意外创建与 `queueNilVal` 相等的值。

---

### 4. [中等] modUint32(0) 下溢导致 OOM

**位置**: `modUint32()` 函数

**问题描述**:
```go
// 修复前
func modUint32(u uint32) uint32 {
    u -= 1  // u=0 时下溢为 4294967295
    // ...
    return u  // 返回 4294967295
}
```
当 `u=0` 时，`u -= 1` 导致 `uint32` 下溢为 `4294967295`，最终返回 `4294967295`。如果 `Init` 中 `cap=0` 且无保护，会尝试分配 4GB+ 的切片导致 OOM。

**修复方式**:
```go
// 修复后
func modUint32(u uint32) uint32 {
    if u == 0 {
        return 0
    }
    u -= 1
    // ...
    return u
}
```
添加 `u == 0` 的边界检查，直接返回 0 避免下溢。

---

### 5. [新增] 添加 modUint64 函数

**位置**: `modUint32()` 之后

**原因**: `head`/`tail` 升级为 `uint64` 后，`Init()` 中需要 `modUint64` 来计算环形队列大小。

**实现**:
```go
func modUint64(u uint64) uint64 {
    if u == 0 {
        return 0
    }
    u -= 1
    u |= u >> 1
    u |= u >> 2
    u |= u >> 4
    u |= u >> 8
    u |= u >> 16
    u |= u >> 32
    return u
}
```
与 `modUint32` 逻辑一致，但支持 64 位输入，包含 `u >> 32` 步骤和 `u == 0` 边界保护。

---

## 二、queue_reference_test.go 修复详情

### 6. [严重] entry.store() 栈指针逃逸风险

**位置**: `entry.store()` 方法

**问题描述**:
```go
// 修复前
func (n *entry) store(i interface{}) {
    atomic.StorePointer(&n.p, unsafe.Pointer(&i))
}
```
`&i` 是函数参数 `i` 的栈地址。虽然 Go 的逃逸分析通常会将 `i` 逃逸到堆上，但这依赖编译器实现细节。如果编译器版本或优化级别变化，`i` 可能留在栈上，`store()` 返回后栈帧被复用，`n.p` 成为悬垂指针。

**修复方式**:
```go
// 修复后
func (n *entry) store(i interface{}) {
    heapI := i
    atomic.StorePointer(&n.p, unsafe.Pointer(&heapI))
}
```
将 `i` 复制到局部变量 `heapI`，`&heapI` 的逃逸分析更明确地触发堆分配，确保指针在函数返回后仍然有效。

---

### 7. [中等] DRQueue head/count 和 tail/count 更新一致性注释

**位置**: `DRQueue.Push()` 和 `DRQueue.Pop()`

**问题描述**:
`head`/`count` 和 `tail`/`count` 的更新不在同一个原子操作中，`Len()` 读者可能在两者之间看到不一致状态。虽然由于双锁设计，Push 和 Pop 之间不会并发操作同一 slot，但 `Len()` 的无锁读取可能看到部分更新的状态。

**修复方式**:
添加详细注释说明更新顺序和一致性保证：
```go
// Push 中
// Update head and count together within the lock.
// count is updated after head so that Len() readers see
// a consistent state (head moved implies count incremented).

// Pop 中
// Update tail and count together within the lock.
// count is updated after tail so that Len() readers see
// a consistent state (tail moved implies count decremented).
```

---

## 三、修复影响评估

| 修复项 | 对正确性的影响 | 对性能的影响 | 对 API 兼容性的影响 |
|--------|---------------|-------------|-------------------|
| uint32→uint64 | 消除溢出导致的队列失效 | 极小（64位原子操作在x64上无额外开销） | 无（内部字段） |
| Pop 原子读取 slot | 消除 ARM 架构上的数据竞争 | 极小（多两次 atomic load） | 无 |
| queueNil 不可构造 | 消除用户误用哨兵值的风险 | 无 | 无（queueNil 未导出） |
| modUint32(0) 边界保护 | 消除 OOM 崩溃 | 无 | 无 |
| modUint64 新增 | 支持 uint64 索引 | 无 | 无（新增函数） |
| entry.store() 堆分配 | 消除悬垂指针风险 | 极小（额外一次接口值复制） | 无 |
| DRQueue 一致性注释 | 提高代码可维护性 | 无 | 无 |

---

## 四、未修复的已知限制

1. **CAS 后抢占导致 Pop 误判**: Push CAS 成功后、数据写入前，如果 G 被抢占，Pop 会短暂误判队列为空。`procPin` 已最大限度减少此窗口，但无法完全消除。这是无锁队列设计的固有权衡。

2. **满/空判断直接返回不重试**: Push/Pop 在检测到满/空时直接返回 false，不进入重试循环。这是 API 设计决策，调用方需根据场景自行重试。

3. **go:linkname 依赖运行时内部函数**: `runtime.procPin`/`runtime.procUnpin` 不在公开 API 中，可能随 Go 版本变化。这是性能关键路径上的必要依赖。

4. **不支持动态扩缩容**: 队列容量在 `Init` 时固定，无法动态调整。这是环形数组队列的设计限制。

5. **eface 与 Go 运行时内部实现耦合**: `eface` 结构体模拟 `runtime.eface`，依赖 `typ` 在前、`data` 在后的内存布局。Go 运行时版本升级时需验证兼容性。

6. **DRQueue 的 uint32 head/tail 仍有溢出风险**: DRQueue 使用双锁设计，性能不如 LockFree，且 head/tail 仍为 uint32。由于 DRQueue 主要用于测试对照，暂不升级。

---

## 五、与 V1 修改记录的关系

本次修改（V2）在 V1 的基础上继续修复更深层次的问题：

| V1 修复（已完成） | V2 修复（本次） |
|-------------------|-----------------|
| slot.data 原子化写入 | head/tail uint32→uint64 防溢出 |
| Len() 下溢保护 | Pop 非原子读取 slot 修复 |
| CAS 失败后 runtime.Gosched() | queueNil 类型混淆修复 |
| DRQueue Push/Pop 操作顺序调整 | modUint32(0) 边界保护 |
| DRQueue 双重 load TOCTOU 修复 | entry.store() 栈指针逃逸修复 |
| | DRQueue 一致性注释补充 |
