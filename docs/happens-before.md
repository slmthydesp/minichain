# Go 里的 happens-before

日期：2026-11-13

一个 goroutine 写下的值，另一个 goroutine 什么时候一定读得到，由 happens-before 规定。两个访问之间有这条关系，读的一方看见写的一方留下的结果。

两个普通访问碰的是同一块内存，至少一个是写，又排不出这条关系，这就是数据竞争。两边都走 `sync/atomic` 时，访问本身就是同步操作。普通读写和原子操作混在同一块内存上，中间又没有 happens-before，仍然是数据竞争。

写的时候按同一个顺序：先把数据写完，再做一次同步；读的一方等对应的同步操作返回，再读数据。通道的发送和接收、锁的 `Unlock` 和 `Lock`、`WaitGroup` 的 `Done` 和 `Wait`、`sync.Once`、`sync/atomic` 会建立这条关系。`time.Sleep` 和普通变量上的忙等不建立。

同一条 goroutine 里面，读自己写过的变量按程序顺序就看得见，不另加锁。

下面按 [Go 内存模型](https://go.dev/ref/mem)（2022-06-06 这一版）说明这些边从哪来。每一节的代码就是文末那个测试文件里的同名函数，或者是单独用 `go run -race` 跑过的缺边片段。验证环境是 Go 1.26.1、darwin/amd64。

## 关系怎么串起来

同一条 goroutine 里，访问按语言规定的求值顺序排，这叫 sequenced before。通道、锁、原子操作这些跨 goroutine 的配对，叫 synchronized before。happens-before 是这两种关系合在一起，再算上传递：A 在 B 之前，B 在 C 之前，那么 A 在 C 之前。

```text
写 msg ──sequenced before──► 发送
                              │
                     synchronized before
                              ▼
读 msg ◄──sequenced before── 接收完成
```

写 `msg` 因此 happens-before 读 `msg`。接收返回之后再读，读到的是发送之前写好的值。下一节的 `TestChannelSendBeforeReceive` 就是这张图。

没有数据竞争的程序是 sequentially consistent 的：结果可以用「所有 goroutine 挪到一个处理器上，按某种交错顺序执行」来解释。这件事一般写成 DRF-SC。

有数据竞争时，实现可以报告竞争并停掉进程，加上 `-race` 就是这么做的。继续跑下去的时候，不超过一个机器字的读只能读到某一次真实写过的值，不会读出一个没人写过的数。比一个机器字大的读写可以拆开。`string`、slice、interface、map 在实现里都是多字（指针配长度，或指针配类型）。竞争可以把这些配对撕开，严重时会破坏内存。并发写 map 时，运行时还会直接 panic。

看见后写的变量，也不包含已经看见先写的变量。下面这段里，`b` 的写入排在 `a` 后面，但两个 goroutine 之间没有同步。内存模型允许先印出 `2`，再印出 `0`。

```go
var a, b int
go func() {
	a = 1
	b = 2
}()
print(b)
print(a)
```

这次在 darwin/amd64 上没有把 `2` 然后 `0` 这个结果跑出来。后面每一段缺边程序上的 `DATA RACE` 是用竞态检测器跑出来的。

内存模型文档里的实用建议是：程序要是必须靠读完整份模型才能自圆其说，这段代码写得太巧了。改成通道或一把锁。

## go 语句

`go` 语句 happens-before 新 goroutine 开始执行。语句之前赋好的值，新 goroutine 里看得见。下面新 goroutine 读到的 `msg` 来自 `go` 之前的赋值，通道只是把这个值送回测试。

```go
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

goroutine 函数返回，不 happens-before 程序里的任何其他事件。写完共享变量就让函数结束，别的 goroutine 没有义务看见这次写。效果传不出去的 `go` 语句，编译器可以整段删掉。

`time.Sleep` 同样不建立这条边。它只把当前 goroutine 挂起一段时间。

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

`GORACE=halt_on_error=1 go run -race` 的报告摘录：

```text
WARNING: DATA RACE
Read at 0x00c000200000 by main goroutine:
  main.main()
      sleep.go:9

Previous write at 0x00c000200000 by goroutine 6:
  main.main.func1()
      sleep.go:7
```

写入在第 7 行，读取在第 9 行。睡了一毫秒，边还是没有。进程退出状态 66。要等对方做完，用后面的 `WaitGroup` 或通道。

## 通道

### 发送排在接收完成之前

对上号的一次发送 happens-before 对应接收完成。缓冲和无缓冲都适用。发送之前写的变量，接收返回之后可以读。

```go
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
```

关闭通道 happens-before 一次因为通道已关闭而返回零值的接收。把上面的发送换成 `close`，读到的还是发送前写好的值。

```go
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
```

### 无缓冲通道上，接收排在发送完成之前

无缓冲通道上，接收 happens-before 对应发送完成。接收之前写的变量，发送返回之后可以读。

```go
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
```

### 容量

容量为 C 时，第 k 次接收 happens-before 第 k+C 次发送完成。无缓冲就是 C = 0，所以上一节是这条规则的特例。

容量 1 时，第 1 次接收 happens-before 第 2 次发送完成。下面先用一次发送把缓冲填满；另一个 goroutine 写 `msg`，再做第 1 次接收；主 goroutine 的第 2 次发送返回之后读 `msg`。

```go
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
```

缓冲通道上，接收若排在写入后面，发送完成就接不上这次写入。对方写完 `msg` 再接收，主 goroutine 第一次发送就能把值放进缓冲。发送返回只说明缓冲里进了一项。

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

`go run -race` 把主 goroutine 的 `println(msg)` 和另一个 goroutine 的 `msg = "hello"` 配成一对，退出状态 66。

容量限制（信号量）用的是同一条规则。`make(chan struct{}, 3)` 上，发送表示占一个名额，接收表示还一个名额。缓冲里已经有 3 个值时，下一次发送是第 k+3 次，要等第 k 次接收 happens-before 它完成。

## 锁

对同一把 `sync.Mutex` 或 `sync.RWMutex`，第 n 次 `Unlock` happens-before 第 m 次 `Lock` 返回，只要 n < m。

下面主 goroutine 先 `Lock`，再启动对方。对方写 `msg`，然后做第一次 `Unlock`。主 goroutine 第二次 `Lock` 返回之后读 `msg`。第一次解锁排在第二次加锁返回之前。

```go
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
```

`RWMutex` 上，某一次 `Unlock` happens-before 之后的 `RLock` 返回；和这次读锁配对的 `RUnlock` happens-before 下一次 `Lock` 返回。`TryLock`、`TryRLock` 成功时与 `Lock`、`RLock` 相同。失败的调用没有同步效果。

锁保证拿着同一把锁的临界区不会同时执行。两边各自加锁时，读的一方可以先拿到锁，读到零值。那次执行没有数据竞争，写操作还没发生。要等对方写完再读，用通道、`WaitGroup`，或像上面这样把锁交出去。

## Once

`once.Do(f)` 里那一次 `f` 执行完，happens-before 任何一次 `Do` 返回。多个 goroutine 同时调用，`f` 只跑一次，其他调用等它返回。两边都在 `Do` 返回之后读 `msg`。

```go
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
```

用普通 `bool` 包住 `Once`，想少进一次 `Do`，会把这条边绕开。`Once` 只同步真正走进 `Do` 的调用。两个 goroutine 同时执行下面这段时，检测器报 `done = true` 和 `if !done` 竞争。验证时外面套了 1000 次循环，并用 `WaitGroup` 等两边都跑起来。

```go
if !done {
	once.Do(func() {
		msg = "hello"
		done = true
	})
}
_ = msg
```

## WaitGroup

`Done` happens-before 被这次 `Done` 放行的 `Wait` 返回。`WaitGroup.Go` 里，`f` 返回 happens-before 被它放行的 `Wait` 返回。下面用的是 `Add` / `Done`。

```go
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
```

计数器为 0 时，正数 `Add` 要发生在 `Wait` 之前，一般写在 `go` 语句前面。同一组 `WaitGroup` 要再用来等下一批事情，新的 `Add` 要等上一轮 `Wait` 已经返回。

## atomic

`sync/atomic` 里的操作都是同步操作。原子操作 A 的结果被原子操作 B 观察到，则 A happens-before B。这些原子操作放在一起看，像是按一个全局顺序执行的，语义与 C++ 的 sequentially consistent 原子操作、Java 的 `volatile` 相同。这个包里没有更弱的内存序可以选。

普通 `bool` 上忙等没有这条边。`done = true` 和 `for !done` 是一对竞争。这个循环也不保证能结束：没有同步事件时，主 goroutine 可以一直看不见那次写。这次检测器配上的就是这两行，进程以状态 66 退出。

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

换成原子标志之后，先写数据，再 `Store`。`Load` 观察到这次存储，再读数据。还没观察到 `true` 的 `Load` 不从这次 `Store` 得到边。

```go
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
```

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

发布整个对象时，字段先填完，再 `Store` 指针。`Load` 得到非 nil 之后，字段已经在这次存储之前写好。

```go
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
```

指针若是普通变量，空转到非 nil 再读字段，指针本身的读写就是竞争。循环即便能退出，看见非 nil 也不包含字段已经写完。

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

## Cond

`Broadcast` 或 `Signal` happens-before 被它唤醒的 `Wait` 返回。`Wait` 返回前会重新锁上 `c.L`。条件在这把锁里修改，也在这把锁里用循环检查：`Wait` 返回时条件可能又变了，而且 `Signal` 若发在 `Wait` 之前，这次信号不会留下来。

```go
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
```

## 包初始化

包 `p` import 包 `q` 时，`q` 的 `init` 全部完成 happens-before `p` 的 `init` 开始。全部 `init` 完成 happens-before `main.main` 开始。

## 写的时候怎么排

1. 两个 goroutine 会碰到同一块内存，并且至少一个在写时，中间放一次同步。
2. 写数据的语句放在同步写前面：发送、`Unlock`、`Done`、`Store`、`Signal`。读数据的语句放在同步读返回后面：接收、下一次 `Lock`、`Wait`、观察到这次存储的 `Load`、被唤醒的 `Cond.Wait`。
3. 要等另一个 goroutine 做完，用 `WaitGroup` 或通道。函数返回、`time.Sleep`、普通变量上的空转，都不算做完。
4. 发布对象时，字段先填完，再用通道、锁或 `atomic.Pointer` 把指针交出去。
5. `Add` 放在启动 goroutine 之前。同一组 `WaitGroup` 再次使用时，等上一轮 `Wait` 返回。
6. 相关包提交前跑 `go test -race`。检测器跟踪 happens-before，缺边会打出 `WARNING: DATA RACE`。

## 统一代码

上面各节里能通过 `-race` 的测试合成这一个文件。验证时它放在模块 `hbcheck` 的 `good` 包里，包名和目录可以换。缺边的片段会报数据竞争，留在对应小节里，不放进这个文件。

```sh
go test -race -count=20 -timeout 60s .
```

结果是 `ok hbcheck/good`，约 1.8 秒。断言检查读到 `"hello"`，`-race` 检查这些访问没有数据竞争。

缺边片段的命令是：

```sh
GORACE=halt_on_error=1 go run -race 文件.go
```

`Sleep`、缓冲通道反方向、原子标志写在数据前面、普通 `bool` 忙等、普通指针、双重检查，每一段都打印了 `WARNING: DATA RACE`，退出状态 66。

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
