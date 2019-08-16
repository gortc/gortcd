package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/runner"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
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
		if err == nil {
			log.Println("resolved", a, "->", addr)
			return addr
		}
		time.Sleep(time.Millisecond * 100 * time.Duration(i))
		log.Printf("unable to resolve %s: %v", a, err)
	}
	panic(fmt.Sprintf("failed to resolve %q", a))
}

type dpLogEntry struct {
	Method string `json:"method"`
	Params struct {
		Args []struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"args"`
	} `json:"params"`
}

func main() {
	flag.Parse()

	log.SetFlags(log.Ltime | log.Lshortfile)
	fmt.Println("bin", *bin, "addr", *httpAddr, "timeout", *timeout)
	client := http.Client{
		Timeout: time.Second * 10,
		Transport: &http.Transport{
			ResponseHeaderTimeout: time.Second * 5,
		},
	}

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

	fs := http.FileServer(http.Dir("static"))
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		log.Println("http:", request.Method, request.URL.Path, request.RemoteAddr)
		fs.ServeHTTP(writer, request)
	})
	gotSuccess := make(chan struct{})
	initialized := make(chan struct{})
	http.HandleFunc("/initialized", func(writer http.ResponseWriter, request *http.Request) {
		log.Println("http:", request.Method, request.URL.Path, request.RemoteAddr)
		defer log.Println("http:", request.Method, request.URL.Path, request.RemoteAddr, "OK")
		switch request.Method {
		case http.MethodPost:
			// Should be called by browser after initializing websocket conn.
			initialized <- struct{}{}
		case http.MethodGet:
			// Should be called by controlling agent to wait until controlled init.
			<-initialized
		}
		_, _ = fmt.Fprintln(writer, "OK")
	})
	http.HandleFunc("/success", func(writer http.ResponseWriter, request *http.Request) {
		gotSuccess <- struct{}{}
	})
	http.HandleFunc("/config", func(writer http.ResponseWriter, request *http.Request) {
		log.Println("http:", request.Method, request.URL.Path, request.RemoteAddr)
		if *controlling {
			// Waiting for controlled agent to start.
			log.Println("waiting for controlled agent init")
			getAddr := resolve("turn-controlled:8080")
			getURL := fmt.Sprintf("http://%s/initialized", getAddr)
			res, getErr := client.Get(getURL)
			if getErr != nil {
				log.Fatalln("failed to get:", getErr)
			}
			if res.StatusCode != http.StatusOK {
				log.Fatalln("bad status", res.Status)
			}
			log.Println("controlled agent initialized")
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
	c, err := chromedp.New(ctx, chromedp.WithLog(func(s string, i ...interface{}) {
		var entry dpLogEntry
		if err := json.Unmarshal([]byte(i[0].(string)), &entry); err != nil {
			log.Fatalln(err)
		}
		if entry.Method == "Runtime.consoleAPICalled" {
			for _, a := range entry.Params.Args {
				log.Println("agent:", a.Value)
			}
		}
	}), chromedp.WithRunnerOptions(
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

	// Checking prometheus metrics.
	log.Println("checking metrics")
	resp, err := client.Get("http://turn-server:3258/metrics")
	if err != nil {
		log.Fatalln("failed to get metrics:", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		log.Println("decoding")
		dec := expfmt.NewDecoder(resp.Body, expfmt.FmtProtoText)
		checked := map[string]bool{
			"gortcd_binding_count":       false,
			"gortcd_permission_count":    false,
			"gortcd_allocation_count":    false,
			"gortcd_stun_messages_count": false,
		}
		for {
			m := new(dto.MetricFamily)
			if err = dec.Decode(m); err != nil {
				if err == io.EOF {
					break
				}
				log.Fatalln("failed to decode metrics:", err)
			}
			switch *m.Name {
			case "gortcd_binding_count", "gortcd_permission_count", "gortcd_allocation_count":
				if *m.Metric[0].Gauge.Value < 1 {
					log.Println("unexpected zero metric value for", *m.Name)
				} else {
					checked[*m.Name] = true
				}
			case "gortcd_stun_messages_count":
				if *m.Metric[0].Counter.Value < 1 {
					log.Println("unexpected zero metric value for", *m.Name)
				} else {
					checked[*m.Name] = true
				}
			}
		}
		failed := false
		for k, v := range checked {
			if !v {
				failed = true
			}
			log.Printf("%30s %s\n", k, map[bool]string{true: "OK", false: "FAILED"}[v])
		}
		if failed {
			log.Fatalln("failed to check metrics")
		} else {
			log.Println("OK")
		}
	default:
		log.Fatalf("bad metrics code: %d", resp.StatusCode)
	}
}
