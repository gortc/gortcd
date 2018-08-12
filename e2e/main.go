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
	httpAddr      = flag.String("addr", "127.0.0.1:8080", "http endpoint to listen")
	signalingAddr = flag.String("signaling", "signaling:2255", "signaling server addr")
	timeout       = flag.Duration("timeout", time.Second*5, "test timeout")
	controlling   = flag.Bool("controlling", false, "agent is controlling")
)

func main() {
	flag.Parse()
	fmt.Println("bin", *bin, "addr", *httpAddr, "timeout", *timeout)
	fs := http.FileServer(http.Dir("static"))
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		log.Println("http:", request.Method, request.URL.Path, request.RemoteAddr)
		fs.ServeHTTP(writer, request)
	})
	gotSuccess := make(chan struct{})
	http.HandleFunc("/success", func(writer http.ResponseWriter, request *http.Request) {
		gotSuccess <- struct{}{}
	})
	http.HandleFunc("/config", func(writer http.ResponseWriter, request *http.Request) {
		log.Println("http:", request.Method, request.URL.Path, request.RemoteAddr)
		for i := 0; i < 10; i++ {
			addr, err := net.ResolveTCPAddr("tcp", *signalingAddr)
			log.Println("resolve:", addr, err)
			if err == nil {
				encoder := json.NewEncoder(writer)
				if encodeErr := encoder.Encode(struct {
					Controlling bool   `json:"controlling"`
					Signaling   string `json:"signaling"`
				}{
					Controlling: *controlling,
					Signaling:   fmt.Sprintf("ws://%s/ws", addr),
				}); encodeErr != nil {
					log.Fatal(err)
				}
				return
			}
			time.Sleep(time.Millisecond * 100 * time.Duration(i))
		}
		log.Fatalln("failed to resolve")
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
