package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"zcode2api/internal/config"
	"zcode2api/internal/gateway"
)

// printBanner mirrors the GLM-ZAI-2API startup banner.
func printBanner() {
	cyan, yellow, magenta, green, reset := "\033[36m", "\033[33m", "\033[35m", "\033[32m", "\033[0m"
	fmt.Println()
	fmt.Println(cyan + "  ███████╗ ██████╗ ██████╗ ██████╗ ███████╗██████╗  █████╗ ██████╗ ██╗" + reset)
	fmt.Println(cyan + "  ╚══███╔╝██╔════╝██╔═══██╗██╔══██╗██╔════╝╚════██║██╔══██╗██╔══██╗██║" + reset)
	fmt.Println(cyan + "    ███╔╝ ██║     ██║   ██║██║  ██║█████╗   █████╔╝███████║██████╔╝██║" + reset)
	fmt.Println(cyan + "   ███╔╝  ██║     ██║   ██║██║  ██║██╔══╝  ██╔═══╝ ██╔══██║██╔═══╝ ██║" + reset)
	fmt.Println(cyan + "  ███████╗╚██████╗╚██████╔╝██████╔╝███████╗███████╗██║  ██║██║     ██║" + reset)
	fmt.Println(cyan + "  ╚═════╝  ╚═════╝ ╚═════╝ ╚═════╝ ╚═════╝ ╚═════╝ ╚═╝  ╚═╝╚═╝     ╚═╝" + reset)
	fmt.Println()
	fmt.Println(yellow + "  📧 Telegram:" + reset + " https://t.me/D3_vin")
	fmt.Println(magenta + "  👤 Author:" + reset + " @D3vin_dev")
	fmt.Println(green + "  🔗 GitHub:" + reset + " https://github.com/D3-vin/Zcode2Api")
	fmt.Println(cyan + "  📦 Version:" + reset + " " + config.AppVersion)
	fmt.Println()
}

func main() {
	printBanner()
	config.LoadDotEnv()
	// LOG_FILE: empty (default) = console only; a path = all log output
	// goes to that file (append) instead of the console
	if lf := strings.TrimSpace(os.Getenv("LOG_FILE")); lf != "" {
		if f, err := os.OpenFile(lf, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			log.SetOutput(f)
			fmt.Println("  logging to " + lf)
		} else {
			fmt.Println("  LOG_FILE unavailable, console logging: " + err.Error())
		}
	}
	// This machine routes egress through a local mitmproxy via env vars
	// (http_proxy=127.0.0.1:18787, SSL_CERT_FILE=mitm CA). zcode.z.ai direct
	// egress serves the real Sectigo chain, so scrub the mitm env: the server
	// must go direct with system roots like in production.
	config.ScrubMitmEnv()
	cfg := config.LoadConfig()
	// Refuse to serve an unauthenticated panel on a publicly reachable socket.
	// Loopback + empty token is allowed (local-only, no auth); anything else
	// without AUTH_TOKEN is a configuration error, not a warning.
	if mode := cfg.AuthMode(); mode == config.AuthModeInvalid {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  ✖ 拒绝启动 / refusing to start: HOST is not loopback and AUTH_TOKEN is empty")
		fmt.Fprintln(os.Stderr, "     HOST="+cfg.Host+" 需要口令，否则任何能访问该端口的人都拿到整个账号池。")
		fmt.Fprintln(os.Stderr, "     (an unauthenticated panel on a routable socket = anyone can export every JWT)")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  设置一个口令 / set one:")
		fmt.Fprintln(os.Stderr, "     AUTH_TOKEN=<随机串>            # 写进 .env")
		fmt.Fprintln(os.Stderr, "     node -e \"console.log(require('crypto').randomBytes(24).toString('base64url'))\"")
		fmt.Fprintln(os.Stderr, "     或者改回 HOST=127.0.0.1（本机免密模式）")
		fmt.Fprintln(os.Stderr)
		os.Exit(1)
	}
	s, err := gateway.NewServer(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	log.Fatal(s.ListenAndServe())
}
