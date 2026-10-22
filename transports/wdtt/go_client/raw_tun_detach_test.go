package main

import (
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

// fakePC — источник, который отдаёт пакеты из канала, а после Close только
// ошибки. Close идемпотентен: один и тот же источник закрывают и шаг теста, и
// уборка.
type fakePC struct {
	pkt  chan byte
	done chan struct{}
	once sync.Once
}

func newFakePC() *fakePC {
	return &fakePC{pkt: make(chan byte, 1), done: make(chan struct{})}
}

func (f *fakePC) ReadFrom(b []byte) (int, net.Addr, error) {
	select {
	case v := <-f.pkt:
		b[0] = v
		return 1, &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)}, nil
	case <-f.done:
		return 0, nil, errors.New("closed")
	}
}
func (f *fakePC) WriteTo([]byte, net.Addr) (int, error) { return 0, nil }
func (f *fakePC) Close() error                          { f.once.Do(func() { close(f.done) }); return nil }
func (f *fakePC) LocalAddr() net.Addr                   { return &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)} }
func (f *fakePC) SetDeadline(time.Time) error           { return nil }
func (f *fakePC) SetReadDeadline(time.Time) error       { return nil }
func (f *fakePC) SetWriteDeadline(time.Time) error      { return nil }

func TestPendingAttachDetachAttach(t *testing.T) {
	p := newPendingPacketConn()
	got := make(chan byte, 4)

	// stop гасит читателя на уборке: без этого горутина остаётся навсегда
	// заблокированной в ReadFrom и держит *testing.T — тот самый шаблон, что
	// даёт «Log in goroutine after test has completed».
	stop := make(chan struct{})
	readerDone := make(chan struct{})
	var sources []*fakePC
	newSource := func() *fakePC {
		f := newFakePC()
		sources = append(sources, f)
		return f
	}

	go func() {
		defer close(readerDone)
		buf := make([]byte, 8)
		for {
			n, _, err := p.ReadFrom(buf)
			if err != nil {
				select {
				case <-stop:
					return // уборка, а не находка
				default:
				}
				t.Errorf("ReadFrom вынес ошибку открепления наружу: %v", err)
				return
			}
			if n > 0 {
				got <- buf[0]
			}
		}
	}()

	t.Cleanup(func() {
		close(stop)
		// Разбудить читателя, где бы он ни стоял: внутри источника — закрытием
		// источников, на ожидании прикрепления — прикреплением мёртвого.
		for _, f := range sources {
			_ = f.Close()
		}
		dead := newFakePC()
		_ = dead.Close()
		p.Attach(dead)
		select {
		case <-readerDone:
		case <-time.After(5 * time.Second):
			t.Error("читатель не завершился — горутина утекла")
		}
	})

	// 1. До Attach читатель обязан блокироваться.
	select {
	case b := <-got:
		t.Fatalf("прочитал %q до Attach", b)
	case <-time.After(100 * time.Millisecond):
	}

	// 2. Первое прикрепление.
	a := newSource()
	p.Attach(a)
	a.pkt <- 'a'
	select {
	case b := <-got:
		if b != 'a' {
			t.Fatalf("после Attach прочитал %q, ждали 'a'", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("после Attach читатель ничего не отдал")
	}

	// 3. Открепление в порядке слота: сначала Detach, потом закрытие
	// дескриптора. Ошибка чтения на закрытом источнике наружу выйти не должна.
	p.Detach()
	_ = a.Close()

	// Читатель обязан снова блокироваться, а не крутиться: канал ready заведён
	// заново. Закрытый ready дал бы busy-loop без единого чтения источника.
	// Состояние ставит сам Detach, поэтому ждать нечего — проверка без пауз.
	p.mu.Lock()
	real, ready := p.real, p.ready
	p.mu.Unlock()
	if real != nil {
		t.Fatal("после Detach источник не отцеплен")
	}
	select {
	case <-ready:
		t.Fatal("после Detach канал ready закрыт — ReadFrom будет крутиться вхолостую")
	default:
	}

	// 4. Повторное прикрепление — Attach обязан закрыть новый ready.
	c := newSource()
	p.Attach(c)
	c.pkt <- 'b'
	select {
	case b := <-got:
		if b != 'b' {
			t.Fatalf("после второго Attach прочитал %q, ждали 'b'", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("после второго Attach читатель ничего не отдал")
	}

	// 5. Attach без Detach между ними не должен ронять процесс закрытием уже
	// закрытого канала. Сегодня слот так не зовёт, но цена ошибки — паника в
	// рабочем процессе, а не отказ команды.
	p.Attach(newSource())
}
