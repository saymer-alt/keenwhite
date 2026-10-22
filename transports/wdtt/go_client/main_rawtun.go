package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

func runRawTunClient(
	ctx context.Context,
	tp *TurnParams,
	peer *net.UDPAddr,
	numW int,
	deviceID, password string,
	stats *Stats,
	pauseFlag *int32,
) {
	pending := newPendingPacketConn()
	stopPending := context.AfterFunc(ctx, func() { _ = pending.Close() })
	defer stopPending()

	disp := NewRawTunDispatcher(ctx, pending, stats)
	defer disp.Shutdown()

	rawConfigCh := make(chan string, 1)
	rawConfigDone := make(chan struct{})
	var tunAttached int32

	go func() {
		defer close(rawConfigDone)
		select {
		case payload, ok := <-rawConfigCh:
			if !ok || payload == "" {
				return
			}
			parts := strings.Split(payload, "|")
			if len(parts) < 3 {
				log.Printf("[RAW] bad config payload: %q", payload)
				return
			}
			mtu, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
			if mtu < 576 {
				mtu = 1300
			}
			conf := RawConf{
				ClientIP: strings.TrimSpace(parts[0]),
				DNS:      strings.TrimSpace(parts[1]),
				MTU:      mtu,
			}

			if awgmEnabled() {
				// Дескриптор приходит командой attach-tun в любой момент —
				// и раньше RAWCONF, и позже. Слот прикрепит его сам, как
				// только появятся обе половины.
				awgmTun.bind(pending)
				log.Printf("[RAW] ожидание TUN от менеджера, клиент %s", conf.ClientIP)
			} else {
				dev, conn, err := startRawTUN(conf)
				if err != nil {
					log.Printf("[RAW] TUN: %v", err)
					return
				}
				_ = dev
				pending.Attach(conn)
				log.Printf("[RAW] TUN готов, клиент %s", conf.ClientIP)
			}

			atomic.StoreInt32(&tunAttached, 1)
			fmt.Printf("RAWCONF|%s|%s|%d\n", conf.ClientIP, conf.DNS, conf.MTU)
			awgmSetAddress(conf.ClientIP, conf.MTU)
		case <-ctx.Done():
		}
	}()

	numGroups := (numW + workersPerGroup - 1) / workersPerGroup
	var wg sync.WaitGroup
	workerIDCounter := 1
	var prevWaitReady <-chan struct{}

	for g := 0; g < numGroups; g++ {
		isFirst := (g == 0)
		var myWaitReady <-chan struct{}
		var mySignalReady chan<- struct{}
		if g > 0 {
			myWaitReady = prevWaitReady
		}
		if g < numGroups-1 {
			ch := make(chan struct{})
			mySignalReady = ch
			prevWaitReady = ch
		}

		startIdx := g * workersPerGroup
		endIdx := startIdx + workersPerGroup
		if endIdx > numW {
			endIdx = numW
		}
		groupSize := endIdx - startIdx
		if groupSize <= 0 {
			continue
		}

		ids := make([]int, groupSize)
		for i := range ids {
			ids[i] = workerIDCounter
			workerIDCounter++
		}

		gID := g + 1
		var cc chan<- string
		if isFirst {
			cc = rawConfigCh
		}

		wg.Add(1)
		go func(groupID int, isFirstGroup bool, configChan chan<- string, workerIds []int, startHashIndex int, waitR <-chan struct{}, sigR chan<- struct{}) {
			defer wg.Done()
			WorkerGroupRaw(ctx, groupID, startHashIndex, tp, peer, disp,
				isFirstGroup, configChan, workerIds, pauseFlag, deviceID, password, stats, waitR, sigR)
		}(gID, isFirst, cc, ids, g, myWaitReady, mySignalReady)
	}

	wg.Wait()
	close(rawConfigCh)
	<-rawConfigDone
	if atomic.LoadInt32(&tunAttached) == 0 {
		log.Println("[RAW] RAWCONF/TUN не получены — выход")
	}
	log.Println("[КЛИЕНТ] Все raw-воркеры завершены")
}
