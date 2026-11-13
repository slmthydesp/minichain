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
	// 保证里面的函数，全局只会执行【一次】，哪怕多 goroutine 并发调用 `once.Do(f)`，常用于**单例初始化、一次性加载配置
	var once sync.Once
	var msg string
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			once.Do(func() {
				msg += "hello"
			})
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
