package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"vortex/engine/runtime/config"
)

func main() {
	configPath := flag.String("config", "", "runtime JSON configuration")
	role := flag.String("role", "", "optional role override")
	flag.Parse()
	if *configPath == "" {
		fatal("--config is required")
	}
	cfg, err := config.LoadFile(*configPath)
	if err != nil {
		fatal(err.Error())
	}
	if *role != "" && config.Role(*role) != cfg.Role {
		fatal("--role does not match config role")
	}
	if _, err := cfg.TLSConfig(cfg.Role != config.RoleSource); err != nil {
		fatal(err.Error())
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	fmt.Printf("vortex-relay ready node_id=%s role=%s listen=%s\n", cfg.NodeID, cfg.Role, cfg.Listen)
	<-ctx.Done()
}

func fatal(message string) {
	_, _ = fmt.Fprintln(os.Stderr, message)
	os.Exit(2)
}
