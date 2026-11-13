# Go 里的 happens-before

日期：2026-11-13

一个 goroutine 写下的值，另一个 goroutine 什么时候一定读得到，由 happens-before 规定。两个访问之间有这条关系，读的一方看见写的一方留下的结果。两个普通访问碰的是同一块内存，至少一个是写，又排不出这条关系，这就是数据竞争。两边都走 `sync/atomic` 时，访问本身就是同步操作。普通读写和原子操作混在同一块内存上，中间又没有 happens-before，仍然是数据竞争。

写的时候按同一个顺序：先把数据写完，再做一次同步；读的一方等对应的同步操作返回，再读数据。通道的发送和接收、锁的 `Unlock` 和 `Lock`、`WaitGroup` 的 `Done` 和 `Wait`、`sync.Once`、`sync/atomic` 会建立这条关系。`time.Sleep` 和普通变量上的忙等不建立。

同一条 goroutine 里面，读自己写过的变量按程序顺序就看得见，不另加锁。

下面按 [Go 内存模型](https://go.dev/ref/mem)（2022-06-06 这一版）说明这些边从哪来。文末的测试在 Go 1.26.1、darwin/amd64 上用 `go test -race -count=20` 通过。几段故意缺边的程序用 `GORACE=halt_on_error=1 go run -race` 跑过，竞态检测器都报了 `WARNING: DATA RACE`，进程退出状态 66。

## 关系怎么串起来

同一条 goroutine 里，访问按语言规定的求值顺序排，这叫 sequenced before。通道、锁、原子操作这些跨 goroutine 的配对，叫 synchronized before。happens-before 是这两种关系合在一起，再算上传递：A 在 B 之前，B 在 C 之前，那么 A 在 C 之前。

```text
写 msg ──sequenced before──► 发送
                              │
                     synchronized before
                              ▼
读 msg ◄──sequenced before── 接收完成
```

写 `msg` 因此 happens-before 读 `msg`。接收返回之后再读，读到的是发送之前写好的值。

没有数据竞争的程序是 sequentially consistent 的：结果可以用「所有 goroutine 挪到一个处理器上，按某种交错顺序执行」来解释。这件事一般写成 DRF-SC。

有数据竞争时，实现可以报告竞争并停掉进程，加上 `-race` 就是这么做的。继续跑下去的时候，不超过一个机器字的读只能读到某一次真实写过的值，不会读出一个没人写过的数。比一个机器字大的读写可以拆开。`string`、slice、interface、map 在实现里都是多字（指针配长度，或指针配类型）。竞争可以把这些配对撕开，严重时会破坏内存。并发写 map 时，运行时还会直接 panic。

内存模型文档里的实用建议是：程序要是必须靠读完整份模型才能自圆其说，这段代码写得太巧了。改成通道或一把锁。

## 同步边从哪来

### go 语句

`go` 语句 happens-before 新 goroutine 开始执行。语句之前赋好的值，新 goroutine 里看得见。`TestGoStatement` 测的是这件事：`msg := "hello"` 写在 `go` 之前，新 goroutine 把 `msg` 送进通道。

goroutine 函数返回，不 happens-before 程序里的任何其他事件。写完共享变量就让函数结束，别的 goroutine 没有义务看见这次写。效果传不出去的 `go` 语句，编译器可以整段删掉。要让别人看见，用下面某一种同步把结果交出去。

### 通道

对上号的一次发送 happens-before 对应接收完成。缓冲和无缓冲都适用。发送之前写的变量，接收返回之后可以读。`TestChannelSendBeforeReceive` 用容量 1 的通道走这条边。

关闭通道 happens-before 一次因为通道已关闭而返回零值的接收。`TestCloseBeforeReceive` 把发送换成 `close`。

无缓冲通道上，接收 happens-before 对应发送完成。接收之前写的变量，发送返回之后可以读。`TestUnbufferedReceiveBeforeSend`：

```go
go func() {
    msg = "hello"
    <-c
}()
c <- struct{}{}
// 发送返回之后读 msg，得到 "hello"
```

容量为 C 时，第 k 次接收 happens-before 第 k+C 次发送完成。无缓冲就是 C = 0，所以第 k 次接收 happens-before 第 k 次发送完成。

容量 1 时，第 1 次接收 happens-before 第 2 次发送完成。`TestBufferedCapacity` 先用一次发送把缓冲填满；另一个 goroutine 写 `msg`，再做第 1 次接收；主 goroutine 的第 2 次发送返回之后读 `msg`。写 `msg` 排在那次接收之前，接收又排在第 2 次发送完成之前，所以读到 `"hello"`。

缓冲通道把方向反过来就接不上。对方写完 `msg` 再接收，主 goroutine 第一次发送就能把值放进缓冲，发送返回只说明缓冲里进了一项，不包含对方已经执行到写入。

```go
var msg string
c := make(chan struct{}, 1)
go func() {
    msg = "hello"
    <-c
}()
c <- struct{}{}
println(msg)
```

`go run -race` 的报告把主 goroutine 的 `println(msg)` 和另一个 goroutine 的 `msg = "hello"` 配成一对。

容量限制（信号量）用的是同一条规则。`make(chan struct{}, 3)` 上，发送表示占一个名额，接收表示还一个名额。缓冲里已经有 3 个值时，下一次发送是第 k+3 次，要等第 k 次接收 happens-before 它完成。

### 锁

对同一把 `sync.Mutex` 或 `sync.RWMutex`，第 n 次 `Unlock` happens-before 第 m 次 `Lock` 返回，只要 n < m。

`TestMutex` 里主 goroutine 先 `Lock`，再启动对方。对方写 `msg`，然后做第一次 `Unlock`。主 goroutine 第二次 `Lock` 返回之后读 `msg`。第一次解锁排在第二次加锁返回之前。

`RWMutex` 上，某一次 `Unlock` happens-before 之后的 `RLock` 返回；和这次读锁配对的 `RUnlock` happens-before 下一次 `Lock` 返回。`TryLock`、`TryRLock` 成功时与 `Lock`、`RLock` 相同。失败的调用没有同步效果。

锁保证拿着同一把锁的临界区不会同时执行。它不保证写的那一方已经先跑过。两边各自加锁时，读的一方可以先拿到锁，读到零值。那次执行没有数据竞争，写操作还没发生。要等对方写完再读，用通道、`WaitGroup`，或像 `TestMutex` 这样把锁交出去。

### Once

`once.Do(f)` 里那一次 `f` 执行完，happens-before 任何一次 `Do` 返回。多个 goroutine 同时调用，`f` 只跑一次，其他调用等它返回。`TestOnce` 里两个 goroutine 都在 `Do` 返回之后读 `msg`。

### WaitGroup

`Done` happens-before 被这次 `Done` 放行的 `Wait` 返回。`WaitGroup.Go` 里，`f` 返回 happens-before 被它放行的 `Wait` 返回。`TestWaitGroup` 用的是 `Add` / `Done`。

计数器为 0 时，正数 `Add` 要发生在 `Wait` 之前，一般写在 `go` 语句前面。同一组 `WaitGroup` 要再用来等下一批事情，新的 `Add` 要等上一轮 `Wait` 已经返回。

### atomic

`sync/atomic` 里的操作都是同步操作。原子操作 A 的结果被原子操作 B 观察到，则 A happens-before B。这些原子操作放在一起看，像是按一个全局顺序执行的，语义与 C++ 的 sequentially consistent 原子操作、Java 的 `volatile` 相同。这个包里没有更弱的内存序可以选。

发布数据时，先把数据写完，再 `Store`。读的一方要等 `Load` 观察到这次存储，再读数据。`TestAtomicPublish` 用 `atomic.Bool` 做标志，`TestAtomicPointer` 用 `atomic.Pointer` 发布整个对象。还没观察到 `true` 的 `Load` 不从这次 `Store` 得到边，所以要在标志变成 true 之后再读 `msg`。

`Store` 若写在数据前面，`Load` 看见 `true` 也接不上数据那次写。检测器报的是 `msg` 上的竞争：写入在 `ready.Store(true)` 之后，读取在 `Load` 返回 true 之后。

```go
var msg string
var ready atomic.Bool
go func() {
    ready.Store(true)
    msg = "hello"
}()
for !ready.Load() {
}
println(msg)
```

### 包初始化和 Cond

包 `p` import 包 `q` 时，`q` 的 `init` 全部完成 happens-before `p` 的 `init` 开始。全部 `init` 完成 happens-before `main.main` 开始。

`sync.Cond` 上，`Broadcast` 或 `Signal` happens-before 被它唤醒的 `Wait` 返回。`Wait` 返回前会重新锁上 `c.L`。条件在这把锁里修改，也在这把锁里用循环检查：`Wait` 返回时条件可能又变了，而且 `Signal` 若发在 `Wait` 之前，这次信号不会留下来。`TestCond` 用 `msg == ""` 做这个循环。

## 缺了同步边的写法

`time.Sleep` 把当前 goroutine 挂起一段时间，和另一个 goroutine 的写入之间没有 happens-before。

```go
package main

import "time"

func main() {
    var msg string
    go func() { msg = "hello" }()
    time.Sleep(time.Millisecond)
    println(msg)
}
```

检测器的报告：

```text
WARNING: DATA RACE
Read at 0x00c000200000 by main goroutine:
  main.main()
      sleep.go:9

Previous write at 0x00c000200000 by goroutine 6:
  main.main.func1()
      sleep.go:7
```

写入在第 7 行，读取在第 9 行。睡了一毫秒，边还是没有。

普通 `bool` 上忙等也没有边。`done = true` 和 `for !done` 是一对竞争。这个循环也不保证能结束：没有同步事件时，主 goroutine 可以一直看不见那次写。

```go
var done bool
var msg string
go func() {
    msg = "hello"
    done = true
}()
for !done {
}
println(msg)
```

这次报告落在 `done = true` 和 `for !done`。进程同样以状态 66 退出，循环没能一直转下去。

普通指针同一类。对象填完后赋给一个普通变量，另一边空转到指针非 nil 再读字段：指针本身的读写就是竞争。循环即便能退出，看见非 nil 也不包含字段已经写完。同步的写法是 `TestAtomicPointer`。

```go
type box struct{ msg string }

var g *box
go func() {
    g = &box{msg: "hello"}
}()
for g == nil {
}
println(g.msg)
```

用普通 `bool` 包住 `sync.Once`，想少进一次 `Do`，会把 `Once` 的边绕开。两个 goroutine 同时执行下面这段时，检测器报 `done = true` 和 `if !done` 竞争。验证时外面套了 1000 次循环，并用 `WaitGroup` 等两边都跑起来。`Once` 只同步真正走进 `Do` 的调用。`TestOnce` 里两边都直接 `Do`，返回之后再读 `msg`。

```go
if !done {
    once.Do(func() {
        msg = "hello"
        done = true
    })
}
_ = msg
```

还有一种模型允许、这次没有单独统计出来的撕开。两个变量按顺序写，另一边先读第二个再读第一个，可以看见第二个已经是新值、第一个还是零值：

```go
var a, b int
go func() {
    a = 1
    b = 2
}()
print(b)
print(a)
```

内存模型允许这里印出 `2` 再印出 `0`。看见 `b == 2` 不包含已经看见 `a == 1`，因为中间没有 happens-before。这次在 darwin/amd64 上没有把 `2` 然后 `0` 这个结果跑出来，上面几段缺边程序上的 `DATA RACE` 是实测的。

## 写代码时怎么排

1. 这块内存会不会有两个 goroutine 碰到，并且至少一个在写。会的话，中间放一次同步。
2. 写数据的语句放在同步写前面：发送、`Unlock`、`Done`、`Store`、`Signal`。读数据的语句放在同步读返回后面：接收、下一次 `Lock`、`Wait`、观察到这次存储的 `Load`、被唤醒的 `Cond.Wait`。
3. 要等另一个 goroutine 做完，用 `WaitGroup` 或通道。函数返回本身不算做完。
4. 发布对象时，字段先填完，再用通道、锁或 `atomic.Pointer` 把指针交出去。
5. `Add` 放在启动 goroutine 之前。同一组 `WaitGroup` 再次使用时，等上一轮 `Wait` 返回。
6. 相关包提交前跑 `go test -race`。检测器跟踪 happens-before，缺边会打出 `WARNING: DATA RACE`。

## 跑过的测试

下面的文件在验证时放在模块 `hbcheck` 的 `good` 包里。包名和目录可以换。执行：

```sh
go test -race -count=20 -timeout 60s .
```

Go 1.26.1、darwin/amd64 上的结果是 `ok hbcheck/good`，约 1.8 秒。断言检查读到 `"hello"`，`-race` 检查这些访问没有数据竞争。

缺边的程序是 `sleep.go`（上面的报告）、缓冲通道反方向、`atomic` 先 `Store` 再写 `msg`、普通 `bool` 忙等、普通指针、双重检查。命令是：

```sh
GORACE=halt_on_error=1 go run -race 文件.go
```

每一段都打印了 `WARNING: DATA RACE`，退出状态 66。

```go
package good

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestChannelSendBeforeReceive(t *testing.T) {
	var msg string
	c := make(chan struct{}, 1)
	go func() {
		msg = "hello"
		c <- struct{}{}
	}()
	<-c
	if msg != "hello" {
		t.Fatalf("got %q", msg)
	}
}

func TestUnbufferedReceiveBeforeSend(t *testing.T) {
	var msg string
	c := make(chan struct{})
	go func() {
		msg = "hello"
		<-c
	}()
	c <- struct{}{}
	if msg != "hello" {
		t.Fatalf("got %q", msg)
	}
}

func TestCloseBeforeReceive(t *testing.T) {
	var msg string
	c := make(chan struct{})
	go func() {
		msg = "hello"
		close(c)
	}()
	<-c
	if msg != "hello" {
		t.Fatalf("got %q", msg)
	}
}

func TestBufferedCapacity(t *testing.T) {
	c := make(chan int, 1)
	var msg string
	c <- 0
	go func() {
		msg = "hello"
		<-c
	}()
	c <- 1
	if msg != "hello" {
		t.Fatalf("got %q", msg)
	}
}

func TestMutex(t *testing.T) {
	var mu sync.Mutex
	var msg string
	mu.Lock()
	go func() {
		msg = "hello"
		mu.Unlock()
	}()
	mu.Lock()
	if msg != "hello" {
		t.Fatalf("got %q", msg)
	}
	mu.Unlock()
}

func TestWaitGroup(t *testing.T) {
	var wg sync.WaitGroup
	var msg string
	wg.Add(1)
	go func() {
		msg = "hello"
		wg.Done()
	}()
	wg.Wait()
	if msg != "hello" {
		t.Fatalf("got %q", msg)
	}
}

func TestOnce(t *testing.T) {
	var once sync.Once
	var msg string
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			once.Do(func() { msg = "hello" })
			if msg != "hello" {
				t.Errorf("got %q", msg)
			}
		}()
	}
	wg.Wait()
}

func TestAtomicPublish(t *testing.T) {
	var msg string
	var ready atomic.Bool
	go func() {
		msg = "hello"
		ready.Store(true)
	}()
	for !ready.Load() {
	}
	if msg != "hello" {
		t.Fatalf("got %q", msg)
	}
}

func TestAtomicPointer(t *testing.T) {
	type box struct{ msg string }
	var p atomic.Pointer[box]
	go func() {
		b := &box{msg: "hello"}
		p.Store(b)
	}()
	var got *box
	for {
		got = p.Load()
		if got != nil {
			break
		}
	}
	if got.msg != "hello" {
		t.Fatalf("got %q", got.msg)
	}
}

func TestCond(t *testing.T) {
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	var msg string
	go func() {
		mu.Lock()
		msg = "hello"
		cond.Signal()
		mu.Unlock()
	}()
	mu.Lock()
	for msg == "" {
		cond.Wait()
	}
	if msg != "hello" {
		t.Fatalf("got %q", msg)
	}
	mu.Unlock()
}

func TestGoStatement(t *testing.T) {
	msg := "hello"
	ch := make(chan string, 1)
	go func() {
		ch <- msg
	}()
	if got := <-ch; got != "hello" {
		t.Fatalf("got %q", got)
	}
}
```
