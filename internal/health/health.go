// Package health implements the `healthcheck` subcommand used by the container
// HEALTHCHECK directive. distroless runtime images ship no shell, curl or wget,
// so the service binary probes its own /healthz endpoint and exits 0 (healthy)
// / 1 (unhealthy):
//
//	HEALTHCHECK CMD ["/app", "healthcheck"]
package health

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

// MaybeProbe runs the healthcheck probe and exits the process, or returns
// immediately when the binary was not invoked as `<bin> healthcheck`. It is a
// no-op unless os.Args[1] == "healthcheck", so it is safe to call at the very
// top of main() — it only needs the already-running HTTP server, not any of its
// dependencies. addr is the server listen address (e.g. ":8080"); path is the
// unauthenticated health endpoint.
func MaybeProbe(addr, path string) {
	if len(os.Args) < 2 || os.Args[1] != "healthcheck" {
		return
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		port = "8080"
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
