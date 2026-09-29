// Command tierlist runs the tier list program: it serves its pages on this
// computer only and opens them in the default browser.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"time"

	"github.com/samtcifihi/tierlist/internal/tierlist"
	"github.com/samtcifihi/tierlist/internal/web"
)

func main() {
	dir := flag.String("dir", "", "`folder` for saved lists (default: tierlist in the user's configuration folder)")
	port := flag.Int("port", 7317, "`port` to serve the pages on, on 127.0.0.1 only")
	noBrowser := flag.Bool("no-browser", false, "don't open the pages in a browser")
	flag.Parse()
	if err := run(*dir, *port, !*noBrowser); err != nil {
		fmt.Fprintln(os.Stderr, "tierlist:", err)
		os.Exit(1)
	}
}

func run(dir string, port int, browser bool) error {
	if dir == "" {
		d, err := tierlist.DefaultDir()
		if err != nil {
			return err
		}
		dir = d
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	url := "http://" + addr + "/"
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// Most likely the program is already running: use that one, so two
		// copies never write the same lists.
		if alreadyRunning(url) {
			fmt.Println("tierlist is already running at", url)
			if browser {
				openBrowser(url)
			}
			return nil
		}
		return fmt.Errorf("can't use port %d, which another program may be using; try -port: %v", port, err)
	}
	s, err := web.New(dir)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	go func() {
		select {
		case <-interrupt:
		case <-s.Quit():
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	fmt.Printf("tierlist is running at %s\nLists are saved in %s\nTo stop it, press Quit on the page, close this window, or press Ctrl+C.\n", url, dir)
	if browser {
		if err := openBrowser(url); err != nil {
			fmt.Println("Open", url, "in your browser.")
		}
	}
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	fmt.Println("tierlist has stopped.")
	return nil
}

// alreadyRunning reports whether tierlist answers at url.
func alreadyRunning(url string) bool {
	c := http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(url + "ping")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	return resp.StatusCode == http.StatusOK && string(body) == "tierlist"
}

// openBrowser opens url in the default browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
