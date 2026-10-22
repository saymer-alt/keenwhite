// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"
)

// Pre-provisioned devices only. AUTH and GETCONF use the same rule; no
// cross-credential claiming of an already provisioned device is permitted.
func serverIdentityValid(id string) bool {
	if len(id) == 0 || len(id) > 64 { return false }
	for _, c := range id { if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') { return false } }
	return true
}
func serverRequestValid(parts []string) bool {
	if len(parts) != 3 || !serverIdentityValid(parts[1]) { return false }
	p, err := strconv.Atoi(parts[0]); return err == nil && p > 0 && p <= 65535
}
func serverAuthorizedLocked(password, id string) bool {
	if db == nil || !serverIdentityValid(id) || db.Devices[id] == nil || password == "" { return false }
	// Legacy owner-password support only for original fixtures. The hardened
	// entry point REFUSES nonempty MainPassword and requires expiring entries.
	if db.MainPassword != "" && password == db.MainPassword { return true }
	e := db.Passwords[password]
	if e == nil || e.IsDeactivated || e.ExpiresAt <= time.Now().Unix() { return false }
	if len(e.DeviceIDs) == 0 { return e.DeviceID == id }
	for _, bound := range e.DeviceIDs { if bound == id { return true } }
	return false
}
func serverAuthorized(password, id string) bool {
	dbMutex.Lock(); defer dbMutex.Unlock(); return serverAuthorizedLocked(password, id)
}
func serverWatchAuthorization(ctx context.Context, cancel context.CancelFunc, c net.Conn, password, id string) {
	t := time.NewTicker(100 * time.Millisecond); defer t.Stop()
	for { select {
	case <-ctx.Done(): return
	case <-t.C:
		if !serverAuthorized(password, id) { cancel(); c.SetDeadline(time.Now()); return }
	} }
}

func serverNoSecretFields(s string) bool { return !strings.ContainsAny(s, "|\r\n") }
