package main

import (
	"fmt"
	"strings"
)

// printNetwork shows the reachability diagnosis of the current community
// server (managers only) and what to do about it.
func (c *cli) printNetwork() error {
	var d struct {
		Verdict  string `json:"verdict"`
		PublicIP string `json:"public_ip"`
		UPnP     *struct {
			Router     string   `json:"router"`
			LocalIP    string   `json:"local_ip"`
			ExternalIP string   `json:"external_ip"`
			Errors     []string `json:"errors"`
			Mapped     []struct {
				Protocol string `json:"protocol"`
				External int    `json:"external"`
			} `json:"mapped"`
		} `json:"upnp"`
		Ports []struct {
			Protocol string `json:"protocol"`
			External int    `json:"external"`
			Internal int    `json:"internal"`
			Purpose  string `json:"purpose"`
		} `json:"ports"`
	}
	if err := c.cdo("GET", "/v1/server/network", nil, &d); err != nil {
		return err
	}
	var ports []string
	for _, p := range d.Ports {
		ports = append(ports, fmt.Sprintf("%d/%s (%s)", p.External, strings.ToLower(p.Protocol), p.Purpose))
	}
	fmt.Printf("Ports à rendre accessibles depuis Internet : %s\n", strings.Join(ports, ", "))
	if d.PublicIP != "" {
		fmt.Printf("Adresse publique vue depuis Internet : %s\n", d.PublicIP)
	}
	if u := d.UPnP; u != nil {
		fmt.Printf("Box (UPnP) : %s — adresse locale du serveur %s, adresse de la box %s, %d port(s) ouvert(s) automatiquement\n",
			u.Router, u.LocalIP, u.ExternalIP, len(u.Mapped))
		for _, e := range u.Errors {
			fmt.Println("  ⚠ " + e)
		}
	}
	fmt.Println()
	switch d.Verdict {
	case "ok":
		fmt.Println("✔ Le serveur devrait être joignable depuis Internet : les ports sont ouverts automatiquement et renouvelés.")
		fmt.Println("  Pour en être sûr, demandez à un ami hors de votre réseau de rejoindre le serveur.")
	case "manual_ports":
		fmt.Println("⚠ Pas d'UPnP : ouvrez (redirigez) vous-même ces ports dans l'interface de votre box, vers l'adresse locale de ce serveur.")
		fmt.Println("  Ou activez UPnP dans la box, puis redémarrez le serveur.")
	case "partial":
		fmt.Println("⚠ Certains ports n'ont pas pu être ouverts (déjà utilisés sur la box ?). Ouvrez-les à la main ou changez-les.")
	case "double_nat":
		fmt.Println("✘ Votre box est elle-même derrière un autre routeur (souvent le « CGNAT » de l'opérateur) : les ports ouverts sur la box")
		fmt.Println("  ne suffisent pas. Solutions : demander une IP publique (souvent gratuite, « IP fixe » / « désactiver CGNAT »")
		fmt.Println("  chez l'opérateur), ou héberger le serveur sur un petit VPS.")
	default:
		fmt.Println("? Adresse publique impossible à déterminer (pas d'accès Internet, ou STUN bloqué).")
	}
	return nil
}
