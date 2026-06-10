# 修改记录 — 内存泄漏与并发访问问题修复

## 修改日期
2025-01-20

## 修改文件
- `queue.go` — LockFree 无锁环形队列核心实现
- `queue_reference_test.go` — DRQueue 双锁环形队列参考实现

---

## 一、queue.go 修复详情

### 1. [严重] Push 中 slot.data 非原子写入（内存泄漏 + 数据竞争）

**位置**: `Push()` 方法，原第 122 行

**问题描述**:
```go
// 修复前
slot.data = (*eface)(unsafe.Pointer(&val)).data
atomic.StorePointer(&slot.typ, (*eface)(unsafe.Pointer(&val)).typ)
```
`slot.data` 是 `unsafe.Pointer` 类型，直接赋值没有使用原子操作，违反了 Go 内存模型对 `unsafe.Pointer` 并发读写的规范。这可能导致：
- GC 追踪到不一致的指针状态，对象无法被正确回收（内存泄漏）
- Pop 侧读取到部分写入的指针值，构造出无效的 `interface{}`

**修复方式**:
```go
// 修复后
atomic.StorePointer(&slot.data, (*eface)(unsafe.Pointer(&val)).data)
atomic.StorePointer(&slot.typ, (*eface)(unsafe.Pointer(&val)).typ)
```
对 `slot.data` 使用 `atomic.StorePointer`，确保写入的原子性和内存可见性，同时保证 GC 能正确追踪 eface 中的指针。

---

### 2. [严重] Pop 中 slot.data 非原子写入（内存泄漏 + 数据竞争）

**位置**: `Pop()` 方法，原第 172 行

**问题描述**:
```go
// 修复前
slot.data = nil
atomic.StorePointer(&slot.typ, nil)
```
与 Push 问题对称，Pop 中清空 `slot.data` 也使用了直接赋值而非原子操作。这可能导致：
- 旧对象的引用残留，GC 无法回收已出队的对象（内存泄漏）
- Push 侧在极端时序下读取到不一致的 eface 状态

**修复方式**:
```go
// 修复后
atomic.StorePointer(&slot.data, nil)
atomic.StorePointer(&slot.typ, nil)
```
对 `slot.data` 的清空使用 `atomic.StorePointer`，确保原子性和 GC 可见性。

---

### 3. [严重] Len() 非原子快照导致下溢返回负数

**位置**: `Len()` 方法，原第 87-89 行

**问题描述**:
```go
// 修复前
func (q *LockFree) Len() int {
    return int(atomic.LoadUint32(&q.head) - atomic.LoadUint32(&q.tail))
}
```
`head` 和 `tail` 分两次原子读取，不是一次快照。在两次读取之间，其他 goroutine 可能修改了 tail，导致 `head - tail` 为负数。由于 `uint32` 下溢，负数会变成一个极大的正数（如 4294967295），调用方依赖 `Len()` 做逻辑判断时会产生错误。

**修复方式**:
```go
// 修复后
func (q *LockFree) Len() int {
    l := int(int32(atomic.LoadUint32(&q.head) - atomic.LoadUint32(&q.tail)))
    if l < 0 {
        return 0
    }
    return l
}
```
通过 `int32` 转换检测下溢，负值 clamp 到 0，避免调用方收到错误的长度值。

---

### 4. [中等] CAS 失败后无限重试导致 CPU 争用

**位置**: `Push()` 和 `Pop()` 方法的 for 循环

**问题描述**:
```go
// 修复前（Push）
for {
    // ...
    runtime_procPin()
    if atomic.CompareAndSwapUint32(&q.head, head, head+1) {
        // ...
        return true
    }
    runtime_procUnpin()
    // 无 yield，立即重试
}
```
在高并发场景下，多个 goroutine 竞争 CAS，失败后立即重试，造成严重的 CPU 争用和活锁倾向。

**修复方式**:
```go
// 修复后（Push）
for {
    // ...
    runtime_procPin()
    if atomic.CompareAndSwapUint32(&q.head, head, head+1) {
        // ...
        return true
    }
    runtime_procUnpin()
    runtime.Gosched() // 让出 CPU，减少争用
}
```
CAS 失败后调用 `runtime.Gosched()` 让出当前 goroutine 的时间片，允许其他 goroutine 运行，减少 CPU 空转。Pop 同理。

---

### 5. [附带] 添加 runtime 包导入

**位置**: import 区域

**原因**: 新增的 `runtime.Gosched()` 调用需要导入 `runtime` 包。

---

## 二、queue_reference_test.go 修复详情

### 1. [严重] Push 先移动 head 后写数据（逻辑不一致）

**位置**: `DRQueue.Push()` 方法，原第 143-144 行

**问题描述**:
```go
// 修复前
atomic.AddUint32(&q.head, 1)  // 先移动 head
slot.store(val)                 // 后写数据
```
Push 先递增 head 再写入数据。Pop 侧通过比较 `head != tail` 判断队列非空，可能看到 head 已移动但 slot 尚未写入的状态，导致 Pop 在队列实际有元素时返回 false。

**修复方式**:
```go
// 修复后
slot.store(val)                 // 先写数据
atomic.AddUint32(&q.head, 1)  // 后移动 head
```
调换顺序，确保 Pop 看到 head 移动时数据已经就绪。

---

### 2. [中等] Pop 双重 load 导致 TOCTOU 问题

**位置**: `DRQueue.Pop()` 方法，原第 165-169 行

**问题描述**:
```go
// 修复前
if slot.load() == nil {   // 第一次 load
    return nil, false
}
val = slot.load()          // 第二次 load
```
`slot.load()` 被调用两次，两次调用之间数据可能发生变化（虽然在双锁设计下同一 slot 不会被并发操作，但逻辑上存在 Time-of-Check-to-Time-of-Use 不一致的风险）。

**修复方式**:
```go
// 修复后
val = slot.load()          // 只 load 一次
if val == nil {
    return nil, false
}
```
只调用一次 `slot.load()`，结果存入 `val` 变量复用，消除 TOCTOU 风险。

---

### 3. [严重] Pop 先移动 tail 后清数据（逻辑不一致）

**位置**: `DRQueue.Pop()` 方法，原第 173-174 行

**问题描述**:
```go
// 修复前
atomic.AddUint32(&q.tail, 1)  // 先移动 tail
slot.free()                     // 后清数据
```
Pop 先递增 tail 再清空 slot。Push 侧通过比较 `head != tail + cap` 判断队列未满，可能看到 tail 已移动但 slot 尚未清空的状态，导致 Push 在队列实际有空位时返回 false。

**修复方式**:
```go
// 修复后
slot.free()                     // 先清数据
atomic.AddUint32(&q.tail, 1)  // 后移动 tail
```
调换顺序，确保 Push 看到 tail 移动时 slot 已经被释放。

---

## 三、修复影响评估

| 修复项 | 对正确性的影响 | 对性能的影响 |
|--------|---------------|-------------|
| slot.data 原子化 | 消除数据竞争和内存泄漏 | 极小（atomic store 在 x86 上几乎无额外开销） |
| Len() 下溢保护 | 消除负数返回 | 无 |
| CAS 失败 yield | 减少活锁倾向 | 高并发下吞吐量提升，低并发下极小延迟增加 |
| DRQueue 操作顺序 | 消除 Pop/Push 误判 | 无 |
| DRQueue 单次 load | 消除 TOCTOU 风险 | 极小（减少一次原子读取） |

---

## 四、未修复的已知限制

1. **队列不缩容**: `Init()` 使用 `sync.Once`，data 切片一旦分配不会释放。对于突发性使用模式可能造成内存浪费，但这属于设计取舍，非 bug。
2. **Len() 非精确**: head 和 tail 仍为分次读取，Len() 返回值为近似值。这是无锁数据结构的固有特性，无法在无锁前提下完全消除。
3. **procPin/procUnpin 间隙**: procPin 防止调度器抢占但不阻止 OS 级信号，极端情况下可能有短暂的可见性延迟。这在实践中影响极小。
