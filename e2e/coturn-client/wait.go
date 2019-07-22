package main

import (
	"fmt"
	"log"
	"net"
	"time"

	"gortc.io/stun"
)

func resolve(a string) *net.UDPAddr {
	for i := 0; i < 10; i++ {
		addr, err := net.ResolveUDPAddr("udp", a)
		if err == nil {
			log.Println("resolved", a, "->", addr)
			return addr
		}
		time.Sleep(time.Millisecond * 100 * time.Duration(i))
	}
	panic("failed to resolve")
}

func main() {
	fmt.Println("waiting for turn server")
	for attempt := 0; attempt < 10; attempt++ {
		c, err := stun.Dial("udp", fmt.Sprintf("turn-server:%d", stun.DefaultPort))
		if err != nil {
			fmt.Println("dial err:", err)
			time.Sleep(time.Millisecond * 100 * time.Duration(attempt))
			continue
		}
		var gotErr error
		err = c.Do(stun.MustBuild(
			stun.TransactionID,
			stun.BindingRequest,
			stun.Fingerprint,
		), func(event stun.Event) {
			gotErr = event.Error
		})
		if err == nil && gotErr == nil {
			fmt.Println("OK")
			break
		}
		if err != nil {
			fmt.Println("dial err:", err)
			time.Sleep(time.Millisecond * 450)
			continue
		}
		if gotErr != nil {
			fmt.Println("event err:", gotErr)
			time.Sleep(time.Millisecond * 450)
			continue
		}
	}
	fmt.Println("waiting for peer")
	resolve("coturn-peer:3478")
	fmt.Println("OK")
}
