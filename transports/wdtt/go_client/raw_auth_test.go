package main

import (
	"context"
	"net"
	"testing"
	"time"
)

// Форма AUTH обязана совпадать с разбором сервера (handleConnRaw:
// strings.TrimPrefix(first, "AUTH:") затем Split по "|"), иначе воркеры
// 2..N не регистрируются и сервер рвёт соединение на первом IP-пакете.
func TestSendAuthPayloadForm(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	errCh := make(chan error, 1)
	go func() { errCh <- SendAuth(client, "dev-42", "s3cret") }()

	buf := make([]byte, 128)
	if err := server.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	n, err := server.Read(buf)
	if err != nil {
		t.Fatalf("чтение AUTH: %v", err)
	}
	if got, want := string(buf[:n]), "AUTH:dev-42|s3cret"; got != want {
		t.Fatalf("payload = %q, ожидалось %q", got, want)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("SendAuth: %v", err)
	}
}

// rawtun обязан раскладывать uplink по 5-tuple hash, иначе TCP-поток
// размазывается по N relay и рвётся на reorder.
func TestRawTunDispatcherUsesFlowHash(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Диспетчеру нужен прикреплённый источник: ReadFrom пустого
	// pendingPacketConn виснет на <-ready, и Shutdown не дожидается readLoop.
	newDisp := func(t *testing.T, make func(context.Context, net.PacketConn, *Stats) *Dispatcher) *Dispatcher {
		t.Helper()
		pc := newPendingPacketConn()
		src := newFakePC()
		pc.Attach(src)
		d := make(ctx, pc, &Stats{})
		t.Cleanup(func() {
			src.Close() // разблокирует readLoop, иначе Shutdown ждёт вечно
			d.Shutdown()
		})
		return d
	}

	if d := newDisp(t, NewRawTunDispatcher); !d.flowHash {
		t.Fatal("NewRawTunDispatcher: flowHash=false, ожидался true")
	}
	if d := newDisp(t, NewDispatcher); d.flowHash {
		t.Fatal("NewDispatcher: flowHash=true, ожидался round-robin")
	}
}
