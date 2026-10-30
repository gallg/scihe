// Command scihe runs Scihe, a modal (vim-style) terminal editor for data
// science: syntax highlighting, which-key, Jupyter notebooks, Lisp REPLs,
// markdown with rendered formulas, git and a CSV/TSV data viewer — in a single
// binary, no plugins.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"

	"github.com/gdamore/tcell/v2"

	"scihe/internal/config"
	"scihe/internal/editor"
)

// version is the release; builds can override it with
// -ldflags "-X main.version=…".
var version = "0.2.0"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	cfgPath := flag.String("config", config.Path(), "configuration file")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: scihe [options] [+LINE] [file ...]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println("scihe", version)
		return
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scihe: config %s: %v\n", *cfgPath, err)
		os.Exit(1)
	}
	editor.SetConfigPath(*cfgPath)

	var files []string
	line := 0
	for _, a := range flag.Args() {
		if strings.HasPrefix(a, "+") {
			if n, err := strconv.Atoi(a[1:]); err == nil {
				line = n
				continue
			}
		}
		files = append(files, a)
	}

	screen, err := tcell.NewScreen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "scihe: %v\n", err)
		os.Exit(1)
	}
	if err := screen.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "scihe: %v\n", err)
		os.Exit(1)
	}
	screen.EnablePaste()
	if cfg.Mouse {
		screen.EnableMouse(tcell.MouseButtonEvents)
	}
	ed := editor.New(screen, cfg)
	// restore the terminal even if the editor panics
	defer func() {
		if r := recover(); r != nil {
			screen.Fini()
			ed.EmergencySave()
			fmt.Fprintf(os.Stderr, "scihe crashed: %v\n%s", r, debug.Stack())
			os.Exit(2)
		}
	}()
	// closing the terminal (SIGHUP) or a kill (SIGTERM): save recovery
	// files and stop the child processes before exiting
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGTERM)
	go func() {
		<-sigs
		signal.Stop(sigs) // a second signal kills the process outright
		ed.Post(ed.Terminate)
	}()
	ed.Open(files)
	if line > 0 {
		ed.GotoLine(line)
	}
	ed.Run()
	screen.Fini()
	for _, n := range ed.ExitNotes() {
		fmt.Fprintln(os.Stderr, n)
	}
}
