package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var httpAddr string

func init() {
	if flag.Lookup("addr") != nil {
		// HACK: For some reason defining "addr" flag fails tests with coverage under some conditions.
		flag.StringVar(&httpAddr, "signaling.addr", "0.0.0.0:2255", "http endpoint to listen")
	} else {
		flag.StringVar(&httpAddr, "addr", "0.0.0.0:2255", "http endpoint to listen")
	}
}

var ws = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var (
	connMux     = new(sync.Mutex)
	connections []*websocket.Conn
)

func main() {
	flag.Parse()
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		panic(err)
	}
	for _, addr := range addresses {
		ip := addr.(*net.IPNet).IP
		if ip.IsLoopback() {
			continue
		}
		fmt.Printf("addr: %s\n", ip)
	}
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		log.Println("WS:", r.RemoteAddr)
		h := http.Header{}
		h.Add("Access-Control-Allow-Origin", "http://127.0.0.1:8080")
		conn, err := ws.Upgrade(w, r, h)
		if err != nil {
			log.Fatalln(err)
		}
		connMux.Lock()
		connections = append(connections, conn)
		connMux.Unlock()
		go func() {
			for {
				t, msg, err := conn.ReadMessage()
				if err != nil {
					break
				}
				connMux.Lock()
				for _, lCon := range connections {
					if lCon == conn {
						continue
					}
					lCon.WriteMessage(t, msg)
				}
				connMux.Unlock()
				log.Println("broadcast:", string(msg), "from", conn.RemoteAddr())
			}
		}()
	})
	log.Fatal(http.ListenAndServe(httpAddr, nil))
}
