// Package netguard tells whether an address is an ordinary public Internet
// address, for the servers' outgoing traffic chosen by others (link
// previews, call relay): never towards the host's own network, this
// machine, or special-purpose ranges that can lead there.
package netguard

import (
	"net"
	"net/netip"
)

// blocked lists the special-purpose ranges (IANA registries) that are not
// ordinary public destinations.
var blocked = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8",       // "this network" (0.0.0.0 reaches this machine)
		"10.0.0.0/8",      // private
		"100.64.0.0/10",   // carrier-grade NAT
		"127.0.0.0/8",     // loopback
		"169.254.0.0/16",  // link-local (cloud metadata services)
		"172.16.0.0/12",   // private
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // documentation
		"192.88.99.0/24",  // 6to4 relay anycast
		"192.168.0.0/16",  // private
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // documentation
		"203.0.113.0/24",  // documentation
		"224.0.0.0/4",     // multicast
		"240.0.0.0/4",     // reserved, and 255.255.255.255 broadcast
		"::/128",          // unspecified
		"::1/128",         // loopback
		"::ffff:0:0/96",   // IPv4-mapped: checked as IPv4 below
		"64:ff9b::/96",    // NAT64: leads to any IPv4, private ones included
		"64:ff9b:1::/48",  // local-use NAT64
		"100::/64",        // discard
		"2001::/32",       // Teredo (embeds an IPv4)
		"2001:db8::/32",   // documentation
		"2002::/16",       // 6to4 (embeds an IPv4)
		"fc00::/7",        // unique local
		"fe80::/10",       // link-local
		"fec0::/10",       // former site-local
		"ff00::/8",        // multicast
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// Public reports whether ip is an ordinary public unicast address.
func Public(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	return PublicAddr(a)
}

// PublicAddr is Public for a netip.Addr.
func PublicAddr(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.Zone() != "" {
		return false
	}
	for _, p := range blocked {
		if p.Contains(a) {
			return false
		}
	}
	return true
}
