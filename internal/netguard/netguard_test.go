package netguard

import (
	"net"
	"testing"
)

func TestPublic(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700::1111": true, "93.184.216.34": true,
		"127.0.0.1": false, "::1": false, "0.0.0.0": false, "0.1.2.3": false,
		"10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false, "100.64.0.1": false,
		"169.254.169.254": false, "198.18.0.1": false, "240.0.0.1": false, "255.255.255.255": false,
		"224.0.0.1": false, "::ffff:127.0.0.1": false, "::ffff:8.8.8.8": true,
		"64:ff9b::a00:1": false, "2002:c0a8:101::1": false, "2001:0:4136:e378::1": false,
		"fd00::1": false, "fe80::1": false, "ff02::1": false, "2001:db8::1": false,
	} {
		if got := Public(net.ParseIP(addr)); got != want {
			t.Errorf("Public(%s) = %v, want %v", addr, got, want)
		}
	}
	if Public(nil) {
		t.Error("nil address is public")
	}
}
