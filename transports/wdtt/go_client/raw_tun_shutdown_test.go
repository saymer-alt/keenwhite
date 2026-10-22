package main

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// shutdownBudget — срок, за который остановка обязана уложиться. Тест на
// зависание сам обязан не висеть: берём меньшее из своего бюджета и остатка
// срока прогона, чтобы падать со своим сообщением, а не по общему таймауту
// пакета (тот печатает дамп горутин и не объясняет, что именно сломано).
func shutdownBudget(t *testing.T) time.Duration {
	t.Helper()
	const want = 5 * time.Second
	dl, ok := t.Deadline()
	if !ok {
		return want
	}
	if left := time.Until(dl) - time.Second; left < want {
		return left
	}
	return want
}

// TestPendingCloseUnblocksReadFrom: Close обязан разбудить читателя, который
// ждёт первого прикрепления. Контракт net.PacketConn: Close снимает
// заблокированные Read/Write с ошибкой. До фикса ReadFrom стоял на <-ready
// вечно, а Close на неприкреплённом слоте был пустышкой.
func TestPendingCloseUnblocksReadFrom(t *testing.T) {
	p := newPendingPacketConn()

	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 8)
		_, _, err := p.ReadFrom(buf)
		errCh <- err
	}()

	// Дать читателю реально встать в ожидание, иначе тест проверял бы быстрый
	// путь «закрыли раньше, чем начали читать».
	time.Sleep(50 * time.Millisecond)

	if err := p.Close(); err != nil {
		t.Fatalf("Close на неприкреплённом слоте вернул ошибку: %v", err)
	}

	budget := shutdownBudget(t)
	select {
	case err := <-errCh:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("ReadFrom после Close вернул %v, ждали net.ErrClosed", err)
		}
	case <-time.After(budget):
		t.Fatalf("ReadFrom не вернулся за %s после Close — ожидающий не разбужен, "+
			"в бою на этом виснет Dispatcher.Shutdown", budget)
	}

	// Идемпотентность: слот закрывают и по отмене контекста, и на уборке.
	if err := p.Close(); err != nil {
		t.Fatalf("повторный Close вернул ошибку: %v", err)
	}

	// После Close писать нельзя: молча принятая запись выглядит как доставка.
	if _, err := p.WriteTo([]byte{1}, &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)}); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("WriteTo после Close вернул %v, ждали net.ErrClosed", err)
	}

	// Attach после Close не воскрешает слот: дескриптор, пришедший в гонке с
	// остановкой, не должен снова открыть чтение.
	f := newFakePC()
	t.Cleanup(func() { _ = f.Close() })
	p.Attach(f)

	buf := make([]byte, 8)
	if _, _, err := p.ReadFrom(buf); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("ReadFrom после Attach-за-Close вернул %v, ждали net.ErrClosed", err)
	}
}

// TestRawTunDispatcherShutdownWithoutTun воспроизводит боевую проводку
// main_rawtun.go: pending + AfterFunc(ctx, Close) + диспетчер. RAWCONF не
// пришёл (сервер недоступен/DENIED/пароль просрочен) — TUN не прикреплён ни
// разу. Отмена контекста (SIGTERM от менеджера) обязана давать чистый выход.
func TestRawTunDispatcherShutdownWithoutTun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pending := newPendingPacketConn()
	stopPending := context.AfterFunc(ctx, func() { _ = pending.Close() })
	defer stopPending()

	disp := NewRawTunDispatcher(ctx, pending, NewStats())

	// Дать readLoop дойти до ReadFrom и встать в ожидание прикрепления. Без
	// паузы отмена успевает раньше первой итерации, readLoop выходит по
	// ctx.Err() в начале цикла, и тест зеленеет на сломанном коде. В бою между
	// стартом и остановкой проходят секунды и минуты — висящий читатель есть
	// всегда.
	time.Sleep(100 * time.Millisecond)

	cancel()

	done := make(chan struct{})
	go func() {
		disp.Shutdown()
		close(done)
	}()

	budget := shutdownBudget(t)
	select {
	case <-done:
	case <-time.After(budget):
		t.Fatalf("Shutdown не вернулся за %s без единого attach-tun — readLoop висит "+
			"в pendingPacketConn.ReadFrom; в бою процесс не завершается по SIGTERM "+
			"и его добивают по таймауту", budget)
	}
}

// TestPendingAttachCloseRace: attach-tun от менеджера может прийти ровно в
// момент остановки. Читатель обязан выйти в любом исходе гонки — если Attach
// успевает сохранить дескриптор уже после Close, слот снова виснет на чтении
// живого fd, и это ровно тот отказ, который чинится этой задачей.
// Объём подобран по мутанту (проверка closed вне mu): на 200 итерациях он
// зеленел, на 20000 падает стабильно; цена на здоровом коде ~0.4 с.
func TestPendingAttachCloseRace(t *testing.T) {
	budget := shutdownBudget(t)
	for i := 0; i < 20000; i++ {
		p := newPendingPacketConn()
		f := newFakePC()

		errCh := make(chan error, 1)
		go func() {
			buf := make([]byte, 8)
			_, _, err := p.ReadFrom(buf)
			errCh <- err
		}()

		start := make(chan struct{})
		attached := make(chan struct{})
		go func() {
			<-start
			p.Attach(f)
			close(attached)
		}()
		close(start)
		_ = p.Close()
		<-attached

		select {
		case <-errCh:
		case <-time.After(budget):
			t.Fatalf("итерация %d: ReadFrom не вернулся за %s при гонке Attach и Close", i, budget)
		}
		_ = f.Close()
	}
}
