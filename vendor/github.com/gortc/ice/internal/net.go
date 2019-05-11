package internal

import "net"

func MustParseNet(n string) *net.IPNet {
	_, parsedNet, err := net.ParseCIDR(n)
	if err != nil {
		panic(err)
	}
	return parsedNet
}
