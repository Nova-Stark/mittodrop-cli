package linkshare

import (
	"fmt"

	"mittodrop/internal/netif"
)

type NetworkLink struct {
	InterfaceName string `json:"interface_name"`
	IP            string `json:"ip"`
	URL           string `json:"url"`
	IsIPv6        bool   `json:"is_ipv6"`
	IsLinkLocal   bool   `json:"is_link_local"`
}

func ResolveLinks(port int) []NetworkLink {
	networks := netif.GetNetworks()
	var links []NetworkLink

	for _, nw := range networks {
		for _, item := range nw.IPtems {
			ipStr := item.IP.String()
			var url string

			if item.IsIPv6 {
				url = fmt.Sprintf("http://[%s]:%d/", ipStr, port)
			} else {
				url = fmt.Sprintf("http://%s:%d/", ipStr, port)
			}

			links = append(links, NetworkLink{
				InterfaceName: nw.InterfaceName,
				IP:            ipStr,
				URL:           url,
				IsIPv6:        item.IsIPv6,
				IsLinkLocal:   item.IsLinkLocal,
			})
		}
	}

	return links
}
