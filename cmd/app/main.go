// Command app runs the migration tool as a local web app: it serves the
// embedded UI on 127.0.0.1 and opens the user's browser.
//
// This wires up the server, static assets, and browser launch only. The HTTP
// API that drives migrate.Pipeline (config, scan, dry-run, commit) is left as a
// follow-up — see TODO(sonnet) below.
package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/exec"
	"runtime"

	"github.com/jolls/mm5-navidrome-migrate/web"
)

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	url := fmt.Sprintf("http://%s/", ln.Addr().String())

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(web.FS)))
	// TODO(sonnet): mount /api/* handlers backed by migrate.Pipeline here
	// (POST config, GET scan, GET dry-run, POST commit).

	log.Printf("serving on %s", url)
	if err := openBrowser(url); err != nil {
		log.Printf("open %s manually (%v)", url, err)
	}
	if err := http.Serve(ln, mux); err != nil {
		log.Fatal(err)
	}
}

// openBrowser opens url in the default browser on Windows and Linux.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	default:
		return errors.New("unsupported platform")
	}
}
