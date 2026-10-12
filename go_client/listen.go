package main

import "net"

// An occupied endpoint is a conflict, including when another socket uses REUSEADDR.
func listenUDP(addr string) (net.PacketConn, error) { return net.ListenPacket("udp4", addr) }
