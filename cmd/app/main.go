// Command app runs the migration tool as a local web app: it serves the
// embedded UI and the /api/* endpoints (config, scan, dry-run, commit) on
// 127.0.0.1, and opens the user's browser.
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
	if runtime.GOOS == "linux" && ensureTerminal() {
		return
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	url := fmt.Sprintf("http://%s/", ln.Addr().String())

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(web.FS)))
	newAPIServer().routes(mux)

	log.Printf("serving on %s", url)
	if err := openBrowser(url); err != nil {
		log.Printf("open %s manually (%v)", url, err)
	} else {
		log.Printf("opened browser at %s", url)
	}
	if err := http.Serve(ln, logRequests(mux)); err != nil {
		log.Fatal(err)
	}
}

// logRequests logs each incoming HTTP request to the terminal so the user
// can see the app is receiving traffic.
func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.RequestURI())
		h.ServeHTTP(w, r)
	})
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
