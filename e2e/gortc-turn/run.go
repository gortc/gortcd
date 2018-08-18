package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"time"
)

func run(ctx context.Context, name string, params ...string) {
	c := exec.CommandContext(ctx, name, params...)
	c.Stderr = os.Stderr
	c.Stdout = os.Stdout
	if err := c.Run(); err != nil {
		log.Fatalln("failed to run", name, params)
	}
}

func captureLogs(ctx context.Context, name string) (*bytes.Buffer, error) {
	captureCtx, _ := context.WithTimeout(ctx, time.Second*5)
	c := exec.CommandContext(captureCtx, "docker", "logs", "ci_turn-"+name+"_1")
	buf := new(bytes.Buffer)
	c.Stderr = buf
	c.Stdout = buf
	if err := c.Run(); err != nil {
		return buf, err
	}
	f, fErr := os.Create("log-" + name + ".txt")
	if fErr == nil {
		f.Write(buf.Bytes())
		f.Close()
	}
	return buf, nil
}

func main() {
	ctx, _ := context.WithTimeout(context.Background(), time.Minute*5)
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		c := exec.CommandContext(cleanupCtx, "docker-compose", "-p", "ci", "kill")
		buf := new(bytes.Buffer)
		c.Stderr = buf
		c.Stdout = buf
		if err := c.Run(); err != nil {
			io.Copy(os.Stdout, buf)
			log.Println("cleanup: failed to kill")
		}
		cancel()
		buf.Reset()

		cleanupCtx, _ = context.WithTimeout(context.Background(), time.Second*5)
		c = exec.CommandContext(cleanupCtx, "docker-compose", "-p", "ci", "rm", "-f")
		c.Stderr = buf
		c.Stdout = buf
		if err := c.Run(); err != nil {
			io.Copy(os.Stdout, buf)
			log.Println("cleanup: failed to rm -f")
		}
	}()
	run(ctx, "docker-compose", "-p", "ci", "build")
	run(ctx, "docker-compose", "-p", "ci", "up", "-d")

	c := exec.CommandContext(ctx, "docker", "wait", "ci_turn-client_1")
	c.Stderr = os.Stderr
	runErr := c.Run()
	captureCtx := context.Background()
	captureLogs(captureCtx, "server")
	captureLogs(captureCtx, "peer")
	if buf, err := captureLogs(captureCtx, "client"); err == nil {
		// Output the logs for the test (for clarity)
		io.Copy(os.Stdout, buf)
	}
	if runErr == nil {
		fmt.Println("OK")
	} else {
		log.Fatalln("Tests Failed -", runErr)
	}
}
