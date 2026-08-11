package health

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

// The distroless runtime image ships no shell, curl or wget, so the container
// HEALTHCHECK runs the service binary against its own /healthz.
func MaybeProbe(addr, path string) {
	if len(os.Args) < 2 || os.Args[1] != "healthcheck" {
		return
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		port = "6734"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	url := "http://" + net.JoinHostPort(host, port) + path

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		os.Exit(1)
	}
	_ = resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		os.Exit(0)
	}
	fmt.Fprintf(os.Stderr, "healthcheck: unhealthy (status %d)\n", resp.StatusCode)
	os.Exit(1)
}
