package tsproxy

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/ge9/socks5"
	"github.com/wlynxg/anet"
	"tailscale.com/net/netmon"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tsnet"
)

// Register anet as the interface getter BEFORE any tailscale code runs.
// init() runs at package load time, before srv.Start() calls netmon.New().
// This sets altNetInterfaces so netInterfaces() always uses anet
// (which reads /proc/net directly) instead of net.Interfaces()
// (which uses netlink, blocked on Android SDK>=30).
func init() {
	netmon.RegisterInterfaceGetter(func() ([]netmon.Interface, error) {
		ifs, err := anet.Interfaces()
		if err != nil {
			return nil, fmt.Errorf("anet.Interfaces: %w", err)
		}
		ret := make([]netmon.Interface, len(ifs))
		for i := range ifs {
			addrs, err := anet.InterfaceAddrsByInterface(&ifs[i])
			if err != nil {
				return nil, fmt.Errorf("ifs[%d].Addrs: %w", i, err)
			}
			ret[i] = netmon.Interface{
				Interface: &ifs[i],
				AltAddrs:  addrs,
			}
		}
		return ret, nil
	})
}

type TsProxy struct {
	tsServer   *tsnet.Server
	tcpTimeout int
	udpTimeout int
	debug      bool
}

func NewTsProxy(tsServer0 *tsnet.Server, tcpTimeout0, udpTimeout0 int, debug0 bool) (*TsProxy, error) {
	t := &TsProxy{
		tcpTimeout: tcpTimeout0,
		udpTimeout: udpTimeout0,
		tsServer:   tsServer0,
		debug:      debug0,
	}
	socks5.Debug = debug0
	// anetPatch() is no longer needed here — init() handles it
	if _, err := t.tsServer.Up(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to start tsnet: %w", err)
	}
	return t, nil
}

// if addr is "*.tshost", resolve it. An IPv4 address is returned.
func resolveTshost(tsServer *tsnet.Server, hostname string, addr string) string {
	host, port, _ := net.SplitHostPort(addr)
	if strings.HasSuffix(host, ".tshost") {
		host = host[:len(host)-7]
		if host == "" {
			host = hostname
		}
		return net.JoinHostPort(resolveTailscaleIPv4(tsServer, host).String(), port)
	}
	return addr
}

func resolveTailscaleIPv4(s *tsnet.Server, hostname string) netip.Addr {
	lc, _ := s.LocalClient()
	status, _ := lc.Status(context.Background())
	for _, peer := range status.Peer {
		if strings.EqualFold(peer.HostName, hostname) {
			return firstIPv4(peer.TailscaleIPs)
		}
	}
	if strings.EqualFold(status.Self.HostName, hostname) {
		return firstIPv4(status.Self.TailscaleIPs)
	}
	panic("couldn't resolve tailscale host: " + hostname)
}

func firstIPv4(ips []netip.Addr) netip.Addr {
	for _, ip := range ips {
		if ip.Is4() {
			return ip
		}
	}
	return netip.Addr{}
}

func isTailscaleHost(s *tsnet.Server, hostname string) bool {
	lc, _ := s.LocalClient()
	status, _ := lc.Status(context.Background())
	for _, peer := range status.Peer {
		if strings.EqualFold(peer.HostName, hostname) || strings.HasPrefix(peer.DNSName, hostname+".") {
			return true
		}
	}
	if strings.EqualFold(status.Self.HostName, hostname) || strings.HasPrefix(status.Self.DNSName, hostname+".") {
		return true
	}
	return false
}

func isTailscaleIPPortString(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return isTailscaleIPString(host)
}

func isTailscaleIPString(host string) bool {
	ip, e := netip.ParseAddr(host)
	if e != nil {
		return false
	}
	return tsaddr.IsTailscaleIP(ip)
}

func isTailscaleIPv4String(host string) bool {
	ip, e := netip.ParseAddr(host)
	if e != nil {
		return false
	}
	return tsaddr.IsTailscaleIPv4(ip)
}

func isTailscaleIPv6String(host string) bool {
	ip, e := netip.ParseAddr(host)
	if e != nil {
		return false
	}
	return tsaddr.TailscaleULARange().Contains(ip)
}

func tsDial(tsServer *tsnet.Server, network, addr string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return tsServer.Dial(ctx, network, addr)
}

func dialAny(tsServer *tsnet.Server, network, addr string) (net.Conn, error) {
	if isTailscaleIPPortString(addr) {
		return tsDial(tsServer, network, addr)
	}
	return net.Dial(network, addr)
}

func listenTCP(tsServer *tsnet.Server, addr string) (net.Listener, error) {
	if isTailscaleIPPortString(addr) {
		return tsServer.Listen("tcp", addr)
	}
	return net.Listen("tcp", addr)
}

func listenUDP(tsServer *tsnet.Server, addr string) (net.PacketConn, error) {
	if isTailscaleIPPortString(addr) {
		return tsServer.ListenPacket("udp", addr)
	}
	return net.ListenPacket("udp", addr)
}
