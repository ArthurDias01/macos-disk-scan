package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"disk-report/internal/config"
	"disk-report/internal/scan"
	"disk-report/internal/server"
	"disk-report/internal/webui"
)

const serveUsage = `Serve the report UI and run scans from it.

  scan serve [options]

Options
  --port <n>           Port to listen on. Default: 7777
  --config <path>      Config file. Default: scan.config.json
  --out <dir>          Snapshot directory. Default: public/data
  --open               Open the app in your browser once it is listening
  --install-agent      Install a LaunchAgent so it starts at login
  --uninstall-agent    Remove the LaunchAgent
  --help               This message

The server binds to 127.0.0.1 only. It moves files to the Trash on request, so
it also requires a per-run token that is injected into the page it serves; a
request from anywhere else is refused.
`

func runServe(argv []string) error {
	set := flag.NewFlagSet("serve", flag.ContinueOnError)
	set.SetOutput(os.Stderr)
	set.Usage = func() { fmt.Fprint(os.Stderr, serveUsage) }

	port := set.Int("port", 7777, "port to listen on")
	configPath := set.String("config", "scan.config.json", "config file")
	outDir := set.String("out", filepath.Join("public", "data"), "snapshot directory")
	openBrowser := set.Bool("open", false, "open the app once listening")
	install := set.Bool("install-agent", false, "install a LaunchAgent")
	uninstall := set.Bool("uninstall-agent", false, "remove the LaunchAgent")

	if err := set.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		return err
	}

	if *uninstall {
		return uninstallAgent()
	}
	if *install {
		return installAgent(*port)
	}

	overrides, err := config.LoadFile(*configPath)
	if err != nil {
		return err
	}
	cfg := config.Resolve(overrides)

	resolvedOut, err := filepath.Abs(*outDir)
	if err != nil {
		return err
	}

	instance, err := server.New(server.Options{
		Port:       *port,
		Config:     cfg,
		ConfigPath: *configPath,
		OutDir:     resolvedOut,
		CachePath:  filepath.Join(scan.CacheDirName, scan.CacheFileName),
		LedgerPath: filepath.Join(scan.CacheDirName, scan.LedgerName),
	})
	if err != nil {
		return err
	}

	// Written for tooling that needs to call the API without scraping the page.
	tokenPath := filepath.Join(scan.CacheDirName, "server-token")
	if err := instance.Token().WriteFile(tokenPath); err != nil {
		return err
	}

	address := fmt.Sprintf("127.0.0.1:%d", *port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w", address, err)
	}

	url := fmt.Sprintf("http://localhost:%d", *port)
	fmt.Fprintf(os.Stderr, "Disk Report listening on %s\n", url)
	fmt.Fprintf(os.Stderr, "Scanning %s · snapshots in %s\n",
		strings.Join(cfg.Roots, ", "), resolvedOut)
	if !webui.Built() {
		fmt.Fprintln(os.Stderr,
			"No UI is embedded in this binary — run `bun run build` and rebuild, "+
				"or use `bun run dev`.")
	}
	fmt.Fprintf(os.Stderr, "Token in %s\n", tokenPath)

	if *openBrowser {
		_ = exec.Command("open", url).Start()
	}

	return http.Serve(listener, instance.Handler())
}

const agentLabel = "com.arthur.disk-report"

func agentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", agentLabel+".plist"), nil
}

// installAgent writes a LaunchAgent that starts the server at login.
//
// The working directory matters: the server resolves scan.config.json,
// .scan-cache/ and public/data relative to it, exactly as the CLI does.
func installAgent(port int) error {
	binary, err := os.Executable()
	if err != nil {
		return err
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		return err
	}

	workdir, err := os.Getwd()
	if err != nil {
		return err
	}

	path, err := agentPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
    <string>--port</string>
    <string>%d</string>
  </array>
  <key>WorkingDirectory</key><string>%s</string>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, agentLabel, binary, port, workdir,
		filepath.Join(workdir, scan.CacheDirName, "serve.log"),
		filepath.Join(workdir, scan.CacheDirName, "serve.log"))

	if err := os.MkdirAll(filepath.Join(workdir, scan.CacheDirName), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return err
	}

	// Replace any previous registration rather than erroring on a duplicate.
	_ = exec.Command("launchctl", "bootout", "gui/"+currentUID()+"/"+agentLabel).Run()
	if output, err := exec.Command(
		"launchctl", "bootstrap", "gui/"+currentUID(), path,
	).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap failed: %s: %w", strings.TrimSpace(string(output)), err)
	}

	fmt.Fprintf(os.Stderr, "Installed %s\n", path)
	fmt.Fprintf(os.Stderr, "Serving %s at login, from %s\n",
		fmt.Sprintf("http://localhost:%d", port), workdir)
	fmt.Fprintln(os.Stderr,
		"This server can move files to the Trash and is now running whenever you "+
			"are logged in. Remove it with `scan serve --uninstall-agent`.")
	return nil
}

func uninstallAgent() error {
	path, err := agentPath()
	if err != nil {
		return err
	}

	_ = exec.Command("launchctl", "bootout", "gui/"+currentUID()+"/"+agentLabel).Run()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}

	fmt.Fprintf(os.Stderr, "Removed %s\n", path)
	return nil
}

func currentUID() string {
	return fmt.Sprint(os.Getuid())
}
