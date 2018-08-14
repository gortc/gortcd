package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/runner"
)

var (
	bin           = flag.String("b", "/usr/bin/google-chrome", "path to binary")
	headless      = flag.Bool("headless", true, "headless mode")
	httpAddr      = flag.String("addr", "0.0.0.0:8080", "http endpoint to listen")
	signalingAddr = flag.String("signaling", "signaling:2255", "signaling server addr")
	timeout       = flag.Duration("timeout", time.Second*5, "test timeout")
	controlling   = flag.Bool("controlling", false, "agent is controlling")
)

func resolve(a string) *net.TCPAddr {
	for i := 0; i < 10; i++ {
		addr, err := net.ResolveTCPAddr("tcp", a)
		log.Println("resolve:", addr, err)
		if err == nil {
			return addr
		}
		time.Sleep(time.Millisecond * 100 * time.Duration(i))
	}
	panic("failed to resolve")
}

func main() {
	flag.Parse()
	fmt.Println("bin", *bin, "addr", *httpAddr, "timeout", *timeout)
	fs := http.FileServer(http.Dir("static"))
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		log.Println("http:", request.Method, request.URL.Path, request.RemoteAddr)
		fs.ServeHTTP(writer, request)
	})
	gotSuccess := make(chan struct{})
	initialized := make(chan struct{})
	http.HandleFunc("/initialized", func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodPost:
			// Should be called by browser after initializing websocket conn.
			initialized <- struct{}{}
		case http.MethodGet:
			// Should be called by controlling agent to wait until controlled init.
			<-initialized
		}
	})
	http.HandleFunc("/success", func(writer http.ResponseWriter, request *http.Request) {
		gotSuccess <- struct{}{}
	})
	http.HandleFunc("/config", func(writer http.ResponseWriter, request *http.Request) {
		log.Println("http:", request.Method, request.URL.Path, request.RemoteAddr)
		if *controlling {
			// Waiting for controlled agent to start.
			getAddr := resolve("turn-controlled:8080")
			getURL := fmt.Sprintf("http://%s/initialized", getAddr)
			res, getErr := http.Get(getURL)
			if getErr != nil {
				log.Fatalln("failed to get:", getErr)
			}
			if res.StatusCode != http.StatusOK {
				log.Fatalln("bad status", res.Status)
			}
		}
		encoder := json.NewEncoder(writer)
		if encodeErr := encoder.Encode(struct {
			Controlling bool   `json:"controlling"`
			Signaling   string `json:"signaling"`
		}{
			Controlling: *controlling,
			Signaling:   fmt.Sprintf("ws://%s/ws", resolve(*signalingAddr)),
		}); encodeErr != nil {
			log.Fatal(encodeErr)
		}
	})
	go func() {
		if err := http.ListenAndServe(*httpAddr, nil); err != nil {
			log.Fatalln("failed to listen:", err)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	c, err := chromedp.New(ctx, chromedp.WithLog(log.Printf), chromedp.WithRunnerOptions(
		runner.Path(*bin), runner.DisableGPU, runner.Flag("headless", *headless),
	))
	if err != nil {
		log.Fatalln("failed to create chrome", err)
	}
	if err := c.Run(ctx, chromedp.Navigate("http://"+*httpAddr)); err != nil {
		log.Fatalln("failed to navigate:", err)
	}
	select {
	case <-gotSuccess:
		log.Println("succeeded")
	case <-ctx.Done():
		log.Fatalln(ctx.Err())
	}
}
