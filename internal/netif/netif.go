package netif

import (
	"log/slog"
	"net"
	"sort"
)


type IPitem struct {
	IP          net.IP
	Mask        net.IPMask 
	IsIPv6      bool
	IsLinkLocal bool 
}

type Network struct {
	InterfaceName string 
	IPtems []IPitem
}

func GetNetworks() ([]Network){
	var networks []Network 
	
	ifaces , err := net.Interfaces()
	if err != nil {
		slog.Warn("Error over finding Networks:" + err.Error())
	}

	for _,iface := range ifaces {
		if iface.Flags & net.FlagUp == 0 {
			continue
		}

		addrs , err := iface.Addrs()
		if err != nil {
			continue 
		}

		var ips []IPitem

		for _ ,addr := range addrs {
			var ip net.IP
			var mask net.IPMask
			switch v:= addr.(type) {
			case *net.IPNet :
				ip = v.IP
				if v.IP.To4() != nil {
					mask = v.Mask
				}
			case *net.IPAddr :
				ip = v.IP
			}

			if ip == nil || ip.IsUnspecified() || ip.IsLoopback() {
				continue
			}

			isLinkLocal := ip.IsLinkLocalUnicast()

			ips = append(ips, IPitem{IP: ip, Mask: mask, IsIPv6: ip.To4() == nil, IsLinkLocal: isLinkLocal})

		}

		if len(ips) != 0 {
			sort.Slice(ips, func(i, j int) bool {
				wi := ipWeight(ips[i])
				wj := ipWeight(ips[j])
				return wi < wj
			})
			networks = append(networks, Network{InterfaceName: string(iface.Name),
			IPtems: ips})
		} 
	}

	return  networks
}

// 0=IPv6 global, 1=IPv4 global, 2=IPv6 link-local, 3=IPv4 link-local.
func ipWeight(item IPitem) int {
	if item.IsIPv6 {
		if item.IsLinkLocal {
			return 2
		}
		return 0
	}
	if item.IsLinkLocal {
		return 3
	}
	return 1
}