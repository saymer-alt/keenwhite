// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServerCredentialPolicy(t *testing.T) {
	for _, tc := range []struct{name string; entry *PasswordEntry; want bool}{
		{"bound", &PasswordEntry{DeviceIDs:[]string{"fixture"},ExpiresAt:time.Now().Add(time.Minute).Unix()},true},
		{"legacy-bound", &PasswordEntry{DeviceID:"fixture",ExpiresAt:time.Now().Add(time.Minute).Unix()},true},
		{"other-device", &PasswordEntry{DeviceIDs:[]string{"other"},ExpiresAt:time.Now().Add(time.Minute).Unix()},false},
		{"stale-legacy-id", &PasswordEntry{DeviceID:"fixture",DeviceIDs:[]string{"other"},ExpiresAt:time.Now().Add(time.Minute).Unix()},false},
		{"unbound", &PasswordEntry{ExpiresAt:time.Now().Add(time.Minute).Unix()},false},
		{"expired", &PasswordEntry{DeviceID:"fixture",ExpiresAt:time.Now().Add(-time.Second).Unix()},false},
		{"boundary-expired", &PasswordEntry{DeviceID:"fixture",ExpiresAt:time.Now().Unix()},false},
		{"unlimited", &PasswordEntry{DeviceID:"fixture"},false},
		{"revoked", &PasswordEntry{DeviceID:"fixture",ExpiresAt:time.Now().Add(time.Minute).Unix(),IsDeactivated:true},false},
	} {
		t.Run(tc.name,func(t *testing.T){ testAUTHDB(t,"AUTH:fixture|synthetic-generated-pass",tc.want,func(){db.MainPassword="";db.Passwords["synthetic-generated-pass"]=tc.entry}) })
	}
	for _, p := range [][]string{{"19000","fixture","pass"},{"0","fixture","pass"},{"65536","fixture","pass"},{"19000","bad/id","pass"},{"19000","fixture","pass","extra"}} {
		want := len(p)==3 && p[0]=="19000" && p[1]=="fixture"
		if serverRequestValid(p)!=want { t.Fatal("malformed GETCONF admission") }
	}
}
func TestActiveRevocation(t *testing.T) {
	db=&Database{Devices:map[string]*ClientDevice{"fixture":{}},Passwords:map[string]*PasswordEntry{"synthetic-generated-pass":{DeviceID:"fixture",ExpiresAt:time.Now().Add(time.Minute).Unix()}}}
	a,b:=net.Pipe();defer a.Close();defer b.Close()
	ctx,cancel:=context.WithCancel(context.Background());defer cancel()
	go serverWatchAuthorization(ctx,cancel,a,"synthetic-generated-pass","fixture")
	dbMutex.Lock();db.Passwords["synthetic-generated-pass"].IsDeactivated=true;dbMutex.Unlock()
	select {case <-ctx.Done():case <-time.After(time.Second):t.Fatal("active session was not revoked")}
	if serverAuthorized("synthetic-generated-pass","fixture") {t.Fatal("revoked session can forward")}
}
func TestRelayPrivateFiles(t *testing.T) {
	d:=t.TempDir();os.Chmod(d,0700);p:=filepath.Join(d,"db.json")
	priv,pub,e:=generateKeyPair();if e!=nil {t.Fatal(e)}
	database:=Database{Devices:map[string]*ClientDevice{"fixture":{DeviceID:"fixture",IP:"10.66.66.2",PrivKey:priv,PubKey:pub}},Passwords:map[string]*PasswordEntry{"synthetic-generated-pass":{DeviceID:"fixture",MaxDevices:1,ExpiresAt:time.Now().Add(time.Minute).Unix()}}}
	raw,_:=json.Marshal(database);os.WriteFile(p,raw,0600)
	if _,e:=relayLoadDB(p);e!=nil {t.Fatal("valid private database rejected")}
	os.Chmod(p,0644);if _,e:=relayLoadDB(p);e==nil {t.Fatal("public database accepted")};os.Chmod(p,0600)
	os.Symlink(p,filepath.Join(d,"alias"));if _,e:=relayRead(filepath.Join(d,"alias"));e==nil {t.Fatal("symlink accepted")}
	os.Link(p,filepath.Join(d,"hard"));if _,e:=relayRead(p);e==nil {t.Fatal("hardlink accepted")};os.Remove(filepath.Join(d,"hard"))
	database.MainPassword="synthetic-master";raw,_=json.Marshal(database);os.WriteFile(p,raw,0600)
	if _,e:=relayLoadDB(p);e==nil {t.Fatal("master password accepted")}
	database.MainPassword="";database.BotToken="synthetic-token";raw,_=json.Marshal(database);os.WriteFile(p,raw,0600)
	if _,e:=relayLoadDB(p);e==nil {t.Fatal("Telegram accepted")}
}
