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

func dockerCompose(ctx context.Context, params ...string) {
	c := exec.CommandContext(ctx, "docker-compose", params...)
	c.Stderr = os.Stderr
	c.Stdout = os.Stdout
	if err := c.Run(); err != nil {
		log.Fatalln("failed to docker-compose", params)
	}
}

func captureLogs(ctx context.Context, name string) (*bytes.Buffer, error) {
	captureCtx, cancel := context.WithTimeout(ctx, time.Second*5)
	defer cancel()
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
	defer cancel()
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()
		c := exec.CommandContext(cleanupCtx, "docker-compose", "--no-ansi", "-p", "ci", "kill")
		buf := new(bytes.Buffer)
		c.Stderr = buf
		c.Stdout = buf
		if err := c.Run(); err != nil {
			io.Copy(os.Stdout, buf)
			log.Println("cleanup: failed to kill")
		}
		cancel()
		buf.Reset()

		cleanupCtx, cancel = context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()
		c = exec.CommandContext(cleanupCtx, "docker-compose", "--no-ansi", "-p", "ci", "rm", "-f")
		c.Stderr = buf
		c.Stdout = buf
		if err := c.Run(); err != nil {
			io.Copy(os.Stdout, buf)
			log.Println("cleanup: failed to rm -f")
		}
	}()

	dockerCompose(ctx, "--no-ansi", "-p", "ci", "build")
	dockerCompose(ctx, "--no-ansi", "-p", "ci", "up", "-d")

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
