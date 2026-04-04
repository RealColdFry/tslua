// tslua-client: tiny Unix socket client for tslua --server.
// Reads JSON request from stdin, sends to socket, prints response to stdout.
package main

import (
	"io"
	"net"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		_, _ = os.Stderr.WriteString("usage: tslua-client <socket-path>\n")
		os.Exit(1)
	}

	conn, err := net.Dial("unix", os.Args[1])
	if err != nil {
		_, _ = os.Stderr.WriteString("connect: " + err.Error() + "\n")
		os.Exit(1)
	}
	defer conn.Close() //nolint:errcheck

	// 10s deadline for the entire request/response cycle
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	// Send stdin to server
	if _, err := io.Copy(conn, os.Stdin); err != nil {
		_, _ = os.Stderr.WriteString("write: " + err.Error() + "\n")
		os.Exit(1)
	}
	_ = conn.(*net.UnixConn).CloseWrite()

	// Read response to stdout
	if _, err := io.Copy(os.Stdout, conn); err != nil {
		_, _ = os.Stderr.WriteString("read: " + err.Error() + "\n")
		os.Exit(1)
	}
}
