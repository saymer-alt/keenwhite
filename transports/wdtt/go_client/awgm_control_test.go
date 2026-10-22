package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Страж пути трафика: слот TUN → fdPacketConn → pendingPacketConn → читатель
// диспетчера. Контракт протокола (state/attach-tun/detach-tun) отвечает верно и
// при разорванном пути — `attached:true` приходит от поля слота, а не от
// прикреплённого дескриптора. Поэтому проверка тут поведенческая: в дескриптор
// пишут, из клиента читают.
//
// Дескриптор берётся из os.Pipe, а не из /dev/net/tun: слоту нужен *os.File, а
// не именно tun, и так тест не требует ни root, ни сетевого namespace.

type readRes struct {
	data []byte
	err  error
}

// readOnce читает один пакет в отдельной горутине. Одноразовая, поэтому на
// зелёном прогоне не остаётся висящих горутин.
func readOnce(p *pendingPacketConn) <-chan readRes {
	ch := make(chan readRes, 1)
	go func() {
		buf := make([]byte, 64)
		n, _, err := p.ReadFrom(buf)
		ch <- readRes{data: append([]byte(nil), buf[:n]...), err: err}
	}()
	return ch
}

func expectPacket(t *testing.T, ch <-chan readRes, want string, whatBroke string) {
	t.Helper()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("%s: чтение вернуло ошибку %v", whatBroke, r.err)
		}
		if string(r.data) != want {
			t.Fatalf("%s: прочитано %q, ждали %q", whatBroke, r.data, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal(whatBroke)
	}
}

// pipeFD отдаёт «дескриптор от менеджера» и конец, в который пишут за него.
func pipeFD(t *testing.T) (fd *os.File, peer *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { _ = w.Close(); _ = r.Close() })
	return r, w
}

// RAWCONF раньше дескриптора: bind уже сделан, attach обязан прикрепить fd
// сразу.
func TestAwgmTunSlotAttachAfterBindDeliversTraffic(t *testing.T) {
	pend := newPendingPacketConn()
	var slot awgmTunSlot
	slot.bind(pend)

	fd, peer := pipeFD(t)
	if err := slot.attach("awgmt-a", fd); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if iface, attached := slot.status(); !attached || iface != "awgmt-a" {
		t.Fatalf("status после attach = %q/%v", iface, attached)
	}

	got := readOnce(pend)
	if _, err := peer.Write([]byte("A")); err != nil {
		t.Fatalf("запись в дескриптор: %v", err)
	}
	expectPacket(t, got, "A",
		"attach не прикрепил дескриптор к клиенту: state скажет attached:true, а трафик не пойдёт")
}

// Дескриптор раньше RAWCONF: attach отработал в пустоту, прикрепить обязан
// bind. Ради этого случая слот и заведён.
func TestAwgmTunSlotBindAfterAttachDeliversTraffic(t *testing.T) {
	pend := newPendingPacketConn()
	var slot awgmTunSlot

	fd, peer := pipeFD(t)
	if err := slot.attach("awgmt-b", fd); err != nil {
		t.Fatalf("attach: %v", err)
	}
	slot.bind(pend)

	got := readOnce(pend)
	if _, err := peer.Write([]byte("B")); err != nil {
		t.Fatalf("запись в дескриптор: %v", err)
	}
	expectPacket(t, got, "B",
		"bind не подобрал дескриптор, пришедший раньше RAWCONF: трафик не пойдёт никогда")
}

// detach обязан открепить клиента, иначе читающий цикл диспетчера останется на
// закрытом дескрипторе и будет жечь CPU до следующего attach-tun.
func TestAwgmTunSlotDetachUnwiresClient(t *testing.T) {
	pend := newPendingPacketConn()
	var slot awgmTunSlot
	slot.bind(pend)

	fd, peer := pipeFD(t)
	if err := slot.attach("awgmt-c", fd); err != nil {
		t.Fatalf("attach: %v", err)
	}
	got := readOnce(pend)
	if _, err := peer.Write([]byte("C")); err != nil {
		t.Fatalf("запись в дескриптор: %v", err)
	}
	expectPacket(t, got, "C", "дескриптор не доехал до клиента ещё до detach")

	slot.detach()

	if iface, attached := slot.status(); attached || iface != "" {
		t.Fatalf("status после detach = %q/%v", iface, attached)
	}
	pend.mu.Lock()
	real, ready := pend.real, pend.ready
	pend.mu.Unlock()
	if real != nil {
		t.Fatal("detach не открепил клиента: диспетчер останется на закрытом дескрипторе и будет крутиться на ошибках")
	}
	select {
	case <-ready:
		t.Fatal("после detach канал ready закрыт — чтение не заблокируется")
	default:
	}

	// Ренумерация OpkgTun17 → OpkgTun18: после detach слот обязан принять новый
	// дескриптор и снова довести трафик.
	fd2, peer2 := pipeFD(t)
	if err := slot.attach("awgmt-c2", fd2); err != nil {
		t.Fatalf("повторный attach: %v", err)
	}
	got2 := readOnce(pend)
	if _, err := peer2.Write([]byte("D")); err != nil {
		t.Fatalf("запись во второй дескриптор: %v", err)
	}
	expectPacket(t, got2, "D", "после detach слот не принимает новый дескриптор: смена интерфейса сломана")
}

// TestAwgmSetupCleanStartLeavesLastErrorEmpty — обратный случай: при исправном
// старте поле молчит.
//
// У этой роли last_error заполняется РОВНО отказами самой обвязки и ничем
// больше (§6.1): классификатора строк журнала нет, и после старта поле молчит
// обо всём. Без этой проверки «всегда непустой last_error» прошёл бы незамеченным.
func TestAwgmSetupCleanStartLeavesLastErrorEmpty(t *testing.T) {
	withAwgmGlobals(t)

	os.Args = []string{"wt-client",
		"--awgm-log-file=" + t.TempDir() + "/awgm.log",
		"-mode", "rawtun",
	}
	rest := awgmSetup()

	// Перезапись argv — условие того, что flag.Parse форка не увидит
	// awgm-флагов и не завершит процесс кодом 2.
	for _, a := range rest {
		if strings.HasPrefix(a, "--awgm-") {
			t.Fatalf("awgm-флаг остался в argv форка: %q", a)
		}
	}
	if len(os.Args) != len(rest)+1 {
		t.Errorf("os.Args не перезаписан: %v", os.Args)
	}

	st := awgmState.snapshot()
	if st.LastError != "" {
		t.Errorf("last_error = %q при исправном старте, ожидали пустую строку", st.LastError)
	}
	if st.Mode != awgmModeRaw {
		t.Errorf("mode = %q, ожидали %q", st.Mode, awgmModeRaw)
	}
	if awgmLog == nil {
		t.Error("журнал открылся, а awgmLog не выставлен")
	}
}

// TestAwgmSetupReportsJournalFailure — недоступный журнал обязан доехать до
// менеджера полем last_error.
//
// Это единственный отказ, о котором роль говорит в last_error, и обойтись
// журналом здесь нельзя по построению: журнала-то и нет. Сообщение уехало бы в
// унаследованный stderr, а менеджер не узнал бы ни причины, ни факта.
func TestAwgmSetupReportsJournalFailure(t *testing.T) {
	withAwgmGlobals(t)

	// Каталога нет — OpenLog отказывает предсказуемо и без прав root.
	os.Args = []string{"wt-client",
		"--awgm-log-file=" + t.TempDir() + "/нет-каталога/awgm.log",
		"-mode", "rawtun",
	}
	awgmSetup()

	// Через JSON: проверяем, что причина именно ДОЕЗЖАЕТ в state, а не просто
	// лежит в поле структуры.
	raw, err := json.Marshal(awgmState.snapshot())
	if err != nil {
		t.Fatalf("сериализация state: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("разбор state: %v", err)
	}
	msg, _ := got["last_error"].(string)
	if msg == "" {
		t.Fatal("last_error пуст при недоступном журнале: менеджер не узнает ни причины, ни факта")
	}
	// Префикс короткий и стабильный — его показывают в интерфейсе, и он не
	// зависит от текста ошибки ядра.
	const prefix = "журнал недоступен: "
	if !strings.HasPrefix(msg, prefix) {
		t.Errorf("last_error = %q, ожидали префикс %q", msg, prefix)
	}
	if awgmLog != nil {
		t.Error("журнал не открылся, а awgmLog выставлен")
	}
}

// TestSetupWritesLastErrorOnce — в awgmSetup ровно одно присвоение lastError.
//
// Отказов обвязки на старте три (журнал, перенаправление вывода, отпечаток
// бинаря), и они копятся в один список: причина слепоты не должна затираться
// причиной помельче. Второе присвоение вернуло бы затирание — молча, потому
// что одновременный отказ двух подсистем в тесте не воспроизвести (BinarySHA256
// отказывает только при недоступном /proc/self/exe).
func TestSetupWritesLastErrorOnce(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "awgm_control.go", nil, 0)
	if err != nil {
		t.Fatalf("разбор awgm_control.go: %v", err)
	}
	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		if d, ok := d.(*ast.FuncDecl); ok && d.Recv == nil && d.Name.Name == "awgmSetup" {
			fn = d
		}
	}
	if fn == nil {
		t.Fatal("в awgm_control.go нет func awgmSetup")
	}

	var at []string
	ast.Inspect(fn, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			sel, ok := lhs.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "lastError" {
				continue
			}
			if x, ok := sel.X.(*ast.Ident); ok && x.Name == "awgmState" {
				at = append(at, fset.Position(as.Pos()).String())
			}
		}
		return true
	})

	if len(at) != 1 {
		t.Fatalf("присвоений awgmState.lastError в awgmSetup: %d (%v), ожидали одно — иначе отказы затирают друг друга", len(at), at)
	}
}

// withAwgmGlobals изолирует тест от глобального состояния обвязки.
//
// Сохраняет и возвращает os.Args, awgmOpts и awgmState, а заодно дескрипторы
// 1 и 2: RedirectStdio внутри awgmSetup накрывает дескрипторы САМОГО процесса,
// а процесс здесь — тестовый бинарь. Без их возврата весь дальнейший вывод
// прогона уходит в журнал, и падение соседнего теста остаётся без текста.
func withAwgmGlobals(t *testing.T) {
	t.Helper()
	restoreStdio(t)
	savedArgs, savedOpts := os.Args, awgmOpts
	t.Cleanup(func() {
		os.Args, awgmOpts, awgmLog = savedArgs, savedOpts, nil
		// Глобальное состояние обвязки после теста обнуляется целиком: в этом
		// пакете его наполняет только awgmSetup, и другого владельца у него нет.
		awgmState.mu.Lock()
		defer awgmState.mu.Unlock()
		awgmState.instance, awgmState.configHash, awgmState.binarySHA256 = "", "", ""
		awgmState.mode, awgmState.lastError = "", ""
		awgmState.address, awgmState.mtu, awgmState.wgConfig = "", 0, ""
	})
}

// restoreStdio запоминает дескрипторы 1 и 2 и возвращает их после теста.
//
// Через syscall, а не через golang.org/x/sys/unix: в go_client x/sys — уже
// косвенная зависимость (единственный прямой импорт снят вместе с
// -tun-fd-sock), и импорт ради теста вернул бы её в прямые, поменяв go.mod.
func restoreStdio(t *testing.T) {
	t.Helper()
	saved := make(map[int]int, 2)
	for _, fd := range []int{1, 2} {
		dup, err := syscall.Dup(fd)
		if err != nil {
			t.Fatalf("сохранение дескриптора %d: %v", fd, err)
		}
		saved[fd] = dup
	}
	t.Cleanup(func() {
		for fd, dup := range saved {
			_ = syscall.Dup3(dup, fd, 0)
			_ = syscall.Close(dup)
		}
	})
}
