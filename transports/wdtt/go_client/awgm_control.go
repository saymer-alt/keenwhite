package main

// Обвязка awg-manager: управляющий сокет, журнал, отпечатки.
// Живёт коммитом в ветке awg-client форка, рядом с исходниками клиента.

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hoaxisr/awg-manager/awgmproto"
)

const (
	awgmImpl = "wt-client"
	awgmRole = "client"
)

// Режимы клиента в терминах протокола. Внутреннее имя форка — rawtun.
const (
	awgmModeRaw = "raw"
	awgmModeWG  = "wg"
)

var (
	awgmOpts    awgmproto.Options
	awgmSrv     *awgmproto.Server
	awgmLog     *awgmproto.Log
	awgmStarted = time.Now()
	awgmState   awgmClientState
	awgmTun     awgmTunSlot
)

// awgmClientState — всё, что процесс рассказывает о себе.
type awgmClientState struct {
	mu           sync.Mutex
	mode         string
	instance     string
	configHash   string
	binarySHA256 string
	address      string
	mtu          int
	wgConfig     string
	lastError    string
}

// awgmSetup — ПЕРВАЯ строка main.
//
// Делает всё до того, как форк тронет свои аргументы: отвечает на пробу
// пригодности, уводит вывод в журнал, считает отпечатки и поднимает
// управляющий сокет. Проба обязана отрабатывать раньше валидации остальных
// аргументов, а flag.Parse на неизвестном флаге печатает usage и завершает
// процесс — поэтому awgm-флаги вырезаются из os.Args здесь же.
//
// Возвращает argv без awgm-флагов; os.Args тоже перезаписывается, чтобы
// сработал глобальный flag.Parse форка.
func awgmSetup() []string {
	full := os.Args[1:]
	rest, opts := awgmproto.SplitArgs(full)
	awgmOpts = opts

	if opts.Protocol {
		if err := awgmproto.PrintProtocol(os.Stdout, awgmproto.ProtocolInfo{
			Impl: awgmImpl, Role: awgmRole, Modes: []string{awgmModeRaw, awgmModeWG},
			Commands: []string{awgmproto.CmdState, awgmproto.CmdAttachTun, awgmproto.CmdDetachTun},
		}); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Запись в закрытый stdout не должна убивать процесс: усыновляемый переживёт
	// смерть менеджера, ручной запуск — закрытый терминал.
	awgmproto.IgnoreSIGPIPE()

	// Отказы обвязки на старте копятся, а не затирают друг друга: без журнала
	// менеджер слепнет целиком, и терять эту причину под отказом отпечатка
	// нельзя. Других писателей lastError, кроме этих трёх мест, нет.
	var setupErrs []string

	if opts.LogFile != "" {
		lg, err := awgmproto.OpenLog(opts.LogFile)
		if err != nil {
			// Журнал не является условием работоспособности туннеля: пишем в
			// stdout, как при ручном запуске, и работаем дальше.
			//
			// Но сказать о нём в last_error обязаны (§6.1). Мотив «видимость
			// ошибок несёт журнал» перестаёт работать ровно тогда, когда журнала
			// нет: сообщение ушло бы в унаследованный stderr, а менеджер не
			// узнал бы вообще ничего. Классификатора строк журнала здесь нет и
			// не появляется: это наш собственный, точно известный отказ нашей же
			// обвязки.
			log.Printf("[AWGM] журнал %s: %v", opts.LogFile, err)
			setupErrs = append(setupErrs, "журнал недоступен: "+err.Error())
		} else {
			awgmLog = lg
			if err := awgmproto.RedirectStdio(lg); err != nil {
				// Слепота та же, а вводит в заблуждение сильнее: файл открыт и
				// пуст, и менеджер прочитает это как «процессу нечего сказать»,
				// а не как «мы ничего не видим».
				log.Printf("[AWGM] вывод в журнал: %v", err)
				setupErrs = append(setupErrs, "вывод не перенаправлен в журнал: "+err.Error())
			}
			go lg.WatchCap(context.Background(), awgmproto.CapPeriod)
		}
	}

	// Отпечатки считаются ОДИН раз при старте. Отпечаток конфигурации — по
	// ПОЛНОМУ argv: ConfigHash сам отбрасывает awgm-флаги, поэтому обе стороны
	// получают одинаковое значение независимо от того, кому что досталось.
	//
	// Отказ BinarySHA256 не фатален: поле уедет ПУСТЫМ, причина — в last_error
	// и в журнале (§5.2 спеки: пустая строка в обязательном поле — «неизвестно»,
	// а не «не совпало»). Падать здесь нельзя — /proc может быть не смонтирован,
	// а туннель к отпечатку отношения не имеет.
	sum, err := awgmproto.BinarySHA256()
	awgmState.mu.Lock()
	if err != nil {
		log.Printf("[AWGM] отпечаток бинаря: %v", err)
		setupErrs = append(setupErrs, "отпечаток бинаря: "+err.Error())
	}
	awgmState.lastError = strings.Join(setupErrs, "; ")
	awgmState.binarySHA256 = sum
	awgmState.configHash = awgmproto.ConfigHash(full)
	awgmState.instance = awgmproto.InstanceFromPath(opts.Socket, awgmImpl, awgmRole)
	// Режим берётся из argv, а не из переменной форка: обвязке он нужен
	// раньше, чем flag.Parse, и опираться на внутренние имена форка не надо.
	awgmState.mode = awgmModeOf(awgmproto.FlagValue(rest, "mode"))
	awgmState.mu.Unlock()

	os.Args = append(os.Args[:1], rest...)
	awgmStartControl()
	return rest
}

// awgmModeOf переводит режим форка в режим протокола.
//
// Нормализация повторяет форк (main.go: пустое и неизвестное значение — vpn):
// rawtun — это raw, vpn — wg. Режим socks в протоколе не описан вовсе;
// менеджер его не запускает, а врать в state при ручном запуске нельзя,
// поэтому он едет как есть — и attach-tun на нём честно отвечает
// not-supported. Объявлять socks режимом wg (как сделала бы проверка «всё, что
// не rawtun») значило бы обещать менеджеру wg-конфиг, которого он не получит.
func awgmModeOf(forkMode string) string {
	switch strings.ToLower(strings.TrimSpace(forkMode)) {
	case "rawtun":
		return awgmModeRaw
	case "socks":
		return "socks"
	default:
		return awgmModeWG
	}
}

// awgmStartControl поднимает управляющий сокет.
func awgmStartControl() {
	if awgmOpts.Socket == "" {
		return // ручной запуск: сокета нет, ведём себя как раньше
	}
	srv, err := awgmproto.Listen(awgmproto.ServerConfig{
		Path: awgmOpts.Socket, Impl: awgmImpl, Role: awgmRole,
		Instance: awgmState.snapshot().Instance,
		Handler:  awgmHandler{},
		OnError:  func(err error) { log.Printf("[AWGM] %v", err) },
	})
	if err != nil {
		// Занятый путь означает, что на инстансе уже работает другой процесс.
		// Безусловный unlink развёл бы два процесса на один инстанс.
		log.Fatalf("[AWGM] управляющий сокет: %v", err)
	}
	awgmSrv = srv
	go srv.Serve()
}

// snapshot собирает ответ на state.
func (s *awgmClientState) snapshot() awgmproto.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := awgmproto.State{
		Role: awgmRole, Mode: s.mode, Instance: s.instance, PID: os.Getpid(),
		ConfigHash: s.configHash, BinarySHA256: s.binarySHA256,
		UptimeS: int64(time.Since(awgmStarted).Seconds()), LastError: s.lastError,
	}
	switch s.mode {
	case awgmModeRaw:
		iface, attached := awgmTun.status()
		st.Tun = &awgmproto.TunState{Iface: iface, Attached: attached}
		// Адрес и MTU выдаёт сервер: до RAWCONF их не существует, и пустое
		// поле честно означает «неизвестно».
		st.Address, st.MTU = s.address, s.mtu
	case awgmModeWG:
		if s.wgConfig != "" {
			st.WG = &awgmproto.WGState{Config: s.wgConfig}
		}
	}
	return st
}

// awgmSetAddress — сервер выдал raw-клиенту адрес.
func awgmSetAddress(address string, mtu int) {
	awgmState.mu.Lock()
	awgmState.address, awgmState.mtu = address, mtu
	awgmState.mu.Unlock()
	awgmPush(awgmproto.Event{Event: awgmproto.EventAddress, Address: address, MTU: mtu})
}

// awgmSetWGConfig — сервер выдал wg-клиенту конфиг. Полезной нагрузки push не
// несёт: конфиг забирается запросом state.
func awgmSetWGConfig(conf string) {
	awgmState.mu.Lock()
	awgmState.wgConfig = conf
	awgmState.mu.Unlock()
	awgmPush(awgmproto.Event{Event: awgmproto.EventWGConfig})
}

// awgmSetError запоминает последнюю ошибку и будит менеджера.
//
// Вызывающего нет — намеренно: общей точки печати ошибок у форка не существует,
// а классификатор строк журнала мы не заводим. Отказы самой обвязки (журнал,
// перенаправление вывода, отпечаток бинаря) пишут lastError прямо в awgmSetup:
// они случаются раньше, чем поднят слушатель, и пушить их было бы некуда.
// Из OnError слушателя её звать НЕЛЬЗЯ: OnError зовётся в том числе из Push, и
// получилась бы рекурсия «push не прошёл → пушим об этом».
func awgmSetError(message string, fatal bool) {
	awgmState.mu.Lock()
	awgmState.lastError = message
	awgmState.mu.Unlock()
	awgmPush(awgmproto.Event{Event: awgmproto.EventError, Message: message, Fatal: fatal})
}

// awgmPushExit — best effort: смерть процесса менеджер и так увидит по
// закрытию соединения, но так он узнает о ней на секунды раньше.
//
// Идёт через PushExit, а не через общий Push: exit стоит на пути выключения, и
// короткий срок записи там не украшение (см. farewellTimeout в библиотеке).
func awgmPushExit(code int) {
	if awgmSrv != nil {
		awgmSrv.PushExit(code)
	}
}

func awgmPush(ev awgmproto.Event) {
	if awgmSrv != nil {
		awgmSrv.Push(ev)
	}
}

// awgmHandler — реализация команд протокола для wt-client.
type awgmHandler struct{}

func (awgmHandler) State() awgmproto.State { return awgmState.snapshot() }

func (awgmHandler) AttachTun(iface string, f *os.File) error {
	if awgmState.snapshot().Mode != awgmModeRaw {
		// TUN есть только у raw-клиента (матрица ролей §6).
		return awgmproto.ErrNotSupported
	}
	return awgmTun.attach(iface, f)
}

func (awgmHandler) DetachTun() error {
	if awgmState.snapshot().Mode != awgmModeRaw {
		return awgmproto.ErrNotSupported
	}
	awgmTun.detach()
	return nil
}

// awgmTunSlot — дескриптор TUN между менеджером и клиентом.
//
// Дескриптор может прийти в любой момент: и до RAWCONF (тогда он ждёт клиента),
// и после (тогда прикрепляется сразу). attached означает «процесс держит
// дескриптор», а не «трафик пошёл»: именно на этот вопрос отвечает контракт,
// и именно из-за этого владения второй TUNSETIFF менеджера даст EBUSY.
type awgmTunSlot struct {
	mu       sync.Mutex
	iface    string
	f        *os.File
	pend     *pendingPacketConn
	attached bool
}

func (s *awgmTunSlot) attach(iface string, f *os.File) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attached {
		// Процесс не подменяет дескриптор молча: смена интерфейса идёт через
		// detach-tun.
		return awgmproto.Errf(awgmproto.CodeBusy, "дескриптор %s уже прикреплён", s.iface)
	}
	s.iface, s.f, s.attached = iface, f, true
	if s.pend != nil {
		s.pend.Attach(newFdPacketConn(f, iface))
	}
	yes := true
	awgmPush(awgmproto.Event{Event: awgmproto.EventTun, Iface: iface, Attached: &yes})
	return nil
}

func (s *awgmTunSlot) detach() {
	s.mu.Lock()
	f, pend, iface := s.f, s.pend, s.iface
	s.f, s.attached, s.iface = nil, false, ""
	s.mu.Unlock()

	if pend != nil {
		pend.Detach()
	}
	if f != nil {
		_ = f.Close()
	}
	no := false
	awgmPush(awgmproto.Event{Event: awgmproto.EventTun, Iface: iface, Attached: &no})
}

// bind связывает слот с клиентом: зовётся, когда пришёл RAWCONF и клиенту есть
// куда прикреплять дескриптор.
func (s *awgmTunSlot) bind(pend *pendingPacketConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pend = pend
	if s.attached && s.f != nil {
		pend.Attach(newFdPacketConn(s.f, s.iface))
	}
}

func (s *awgmTunSlot) status() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.iface, s.attached
}

// awgmEnabled — работаем ли под менеджером.
func awgmEnabled() bool { return awgmOpts.Socket != "" }
