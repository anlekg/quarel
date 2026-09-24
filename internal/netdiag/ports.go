package netdiag

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Describe lists ports for people: "7882/udp (voice), …".
func Describe(ports []Mapping) string {
	var parts []string
	for _, p := range ports {
		parts = append(parts, fmt.Sprintf("%d/%s (%s)", p.External, strings.ToLower(p.Protocol), p.Purpose))
	}
	return strings.Join(parts, ", ")
}

// OpenPorts maps ports on the router through UPnP and keeps them alive until
// ctx ends (then removes them). nil when no router answered.
func OpenPorts(ctx context.Context, ports []Mapping, lease time.Duration, wg *sync.WaitGroup) *PortMapper {
	gw, err := Discover(ctx)
	if err != nil {
		slog.Warn("UPnP: "+err.Error()+"; open these ports on the router yourself", "ports", Describe(ports))
		return nil
	}
	pm := NewPortMapper(gw, ports, lease)
	st := pm.Refresh(ctx)
	slog.Info("UPnP: ports opened on the router", "router", st.Router, "local_ip", st.LocalIP, "external_ip", st.ExternalIP, "mapped", Describe(st.Mapped))
	if len(st.Errors) > 0 {
		slog.Warn("UPnP: some ports could not be opened", "errors", st.Errors)
	}
	if IsPrivate(st.ExternalIP) {
		slog.Warn("UPnP: the router's own address is private: this connection is behind another NAT (often carrier-grade NAT); these ports are probably NOT reachable from the Internet", "external_ip", st.ExternalIP)
	}
	wg.Add(1)
	go func() { defer wg.Done(); pm.Run(ctx) }() // removes the mappings when ctx ends
	return pm
}
