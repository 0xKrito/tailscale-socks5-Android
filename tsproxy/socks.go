package tsproxy

import (
	"errors"
	"log"
	"net"
	"time"

	"github.com/ge9/socks5"
)

var socksLogf func(format string, args ...any)

func SetSocksLogf(fn func(format string, args ...any)) {
	socksLogf = fn
}

func socksLog(format string, args ...any) {
	if socksLogf != nil {
		socksLogf("[SOCKS5] "+format, args...)
	}
	log.Printf("[SOCKS5] "+format, args...)
}

// socks5Server is the running SOCKS5 server instance.
// Set when ServeSOCKS starts so StopSocks5() can shut it down.
var socks5Server *socks5.Server

// StopSocks5 shuts down the running SOCKS5 server, releasing the port.
// Safe to call when no server is running.
func StopSocks5() {
	s := socks5Server
	if s == nil {
		return
	}
	socks5Server = nil // prevent re-entrance
	socksLog("Shutting down SOCKS5...")
	// RunnerGroup.Done() calls Stop() on each runner (which closes listeners)
	// then waits for all goroutines to exit.
	// Run in goroutine with timeout to avoid blocking forever if Accept() is stuck.
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.RunnerGroup.Done()
	}()
	select {
	case <-done:
		socksLog("SOCKS5 shut down cleanly")
	case <-time.After(5 * time.Second):
		socksLog("SOCKS5 shutdown timed out (5s), port may be busy briefly")
	}
}

func (t *TsProxy) baseSOCKSConfig(bind string) *socks5.Server {
	bind = resolveTshost(t.tsServer, t.tsServer.Hostname, bind)
	h, _, _ := net.SplitHostPort(bind)
	if h == "" {
		h = "0.0.0.0"
	}
	server, _ := socks5.NewClassicServer(bind, h, "", "", t.tcpTimeout, t.udpTimeout)
	server.ListenTCP = func(_ string, laddr string) (net.Listener, error) {
		socksLog("ListenTCP called: laddr=%s", laddr)
		ln, err := listenTCP(t.tsServer, laddr)
		if err != nil {
			socksLog("ListenTCP error: %v", err)
		} else {
			socksLog("ListenTCP OK: %s", ln.Addr())
		}
		return ln, err
	}
	server.ListenUDP = func(network, laddr string) (net.PacketConn, error) {
		socksLog("ListenUDP called: network=%s laddr=%s", network, laddr)
		pc, err := listenUDP(t.tsServer, laddr)
		if err != nil {
			socksLog("ListenUDP error: %v", err)
		} else {
			socksLog("ListenUDP OK: %s", pc.LocalAddr())
		}
		return pc, err
	}
	return server
}

func (t *TsProxy) ServeSOCKS(bind, tcp4, tcp6, udp4, udp6 string) {
	server := t.baseSOCKSConfig(bind)
	socks5Server = server

	server.DialTCP = func(network string, _, raddr string) (net.Conn, error) {
		socksLog("DialTCP: %s %s", network, raddr)

		host, _, err := net.SplitHostPort(raddr)
		if err != nil {
			socksLog("SplitHostPort error: %v", err)
			return nil, err
		}

		// If the target is a Tailscale IP or hostname, use tsnet Dial
		if isTailscaleIPString(host) || isTailscaleHost(t.tsServer, host) {
			socksLog("Using tsnet Dial for Tailscale target: %s", raddr)
			conn, err := tsDial(t.tsServer, network, raddr)
			if err != nil {
				socksLog("tsnet Dial error: %v", err)
			} else {
				socksLog("tsnet Dial OK: %s -> %s", conn.LocalAddr(), conn.RemoteAddr())
			}
			return conn, err
		}

		// For non-Tailscale targets, use standard dial
		ra, err := net.ResolveTCPAddr("tcp", raddr)
		if err != nil {
			socksLog("ResolveTCPAddr error: %v", err)
			return nil, err
		}
		a2, _ := net.ResolveTCPAddr("tcp", tcp6)
		if ra.IP.To4() != nil {
			a2, _ = net.ResolveTCPAddr("tcp", tcp4)
		}
		socksLog("Dialing %s via %s (standard)", ra.String(), network)
		conn, err := net.DialTCP(network, a2, ra)
		if err != nil {
			socksLog("DialTCP error: %v", err)
		} else {
			socksLog("DialTCP OK: %s -> %s", conn.LocalAddr(), conn.RemoteAddr())
		}
		return conn, err
	}

	server.BindOutUDP = func(network string, laddr string) (net.PacketConn, error) {
		return socks5.BindOutUDP(network, laddr)
	}
	if udp4 == "disabled" || udp6 == "disabled" {
		udpOut := udp4
		if udp4 == "disabled" {
			udpOut = udp6
		}
		server.BindOutUDP = func(network string, laddr string) (net.PacketConn, error) {
			var la *net.UDPAddr
			if laddr != "" {
				var err error
				la, err = net.ResolveUDPAddr(network, laddr)
				if err != nil {
					return nil, err
				}
			} else {
				la, _ = net.ResolveUDPAddr(network, udpOut)
			}
			return net.ListenUDP(network, la)
		}
	} else if udp4 != "" || udp6 != "" {
		server.BindOutUDP = func(network string, laddr string) (net.PacketConn, error) {
			co := func(dstAddr net.Addr) (net.PacketConn, error) {
				network, address := "udp6", udp6
				if dstAddr.(*net.UDPAddr).IP.To4() != nil {
					network, address = "udp4", udp4
				}
				if t.debug {
					log.Printf("[delayedUDPConn initialized]: %s, BindAddr: %s, Dest: %s\n", network, address, dstAddr)
				}
				return net.ListenPacket(network, address)
			}
			return &delayedUDPConn{connOpener: co}, nil
		}
	}

	socksLog("Calling ListenAndServe on %s", bind)
	err := server.ListenAndServe(nil)
	socksLog("ListenAndServe returned: %v", err)
	socks5Server = nil
}

func (t *TsProxy) ForwardSOCKS(bind, connect string) {
	server := t.baseSOCKSConfig(bind)
	connect = resolveTshost(t.tsServer, t.tsServer.Hostname, connect)
	client, _ := socks5.NewClient(connect, "", "", t.tcpTimeout, t.udpTimeout)
	client.DialTCP = func(network string, laddr, raddr string) (net.Conn, error) {
		a, err := net.ResolveTCPAddr(network, raddr)
		if err != nil {
			return nil, err
		}
		return tsDial(t.tsServer, network, a.String())
	}
	server.DialTCP = func(network string, _, raddr string) (net.Conn, error) {
		a, err := net.ResolveTCPAddr(network, raddr)
		if err != nil {
			return nil, err
		}
		return client.Dial(network, a.String())
	}
	server.BindOutUDP = func(network string, _ string) (net.PacketConn, error) {
		if err := client.Negotiate(nil); err != nil {
			return nil, err
		}
		a, h, p := socks5.ATYPIPv4, []byte{0x00, 0x00, 0x00, 0x00}, []byte{0x00, 0x00}
		rp, err := client.Request(socks5.NewRequest(socks5.CmdUDP, a, h, p))
		if err != nil {
			return nil, err
		}
		c, err := tsDial(t.tsServer, "udp", rp.Address())
		uc := proxyUDPConn{UDPConn: c}
		return uc, err
	}
	server.ListenAndServe(nil)
}

func (t *TsProxy) TailnetSOCKS(bind string) {
	server := t.baseSOCKSConfig(bind)
	server.DialTCP = func(network string, _, raddr string) (net.Conn, error) {
		return tsDial(t.tsServer, network, raddr)
	}
	server.BindOutUDP = func(network string, laddr string) (net.PacketConn, error) {
		return t.tsServer.ListenPacket(network, laddr)
	}
	server.ListenAndServe(nil)
}

func (t *TsProxy) DualSOCKS(bind, tcp4, tcp6, udp4, udp6 string) {
	server := t.baseSOCKSConfig(bind)
	server.DialTCP = func(network string, _, raddr string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(raddr)
		if isTailscaleHost(t.tsServer, host) || isTailscaleIPString(host) {
			return tsDial(t.tsServer, network, raddr)
		}
		ra, err := net.ResolveTCPAddr("tcp", raddr)
		if err != nil {
			return nil, err
		}
		a2, _ := net.ResolveTCPAddr("tcp", tcp6)
		if ra.IP.To4() != nil {
			a2, _ = net.ResolveTCPAddr("tcp", tcp4)
		}
		return net.DialTCP(network, a2, ra)
	}
	server.BindOutUDP = func(network string, laddr string) (net.PacketConn, error) {
		ts4, ts6 := t.tsServer.TailscaleIPs()
		co := func(dstAddr net.Addr) (net.PacketConn, error) {
			if isTailscaleIPv4String(dstAddr.Network()) {
				return t.tsServer.ListenPacket("udp", ts4.String())
			}
			if isTailscaleIPv6String(dstAddr.Network()) {
				return t.tsServer.ListenPacket("udp", ts6.String())
			}
			network, address := "udp6", udp6
			if dstAddr.(*net.UDPAddr).IP.To4() != nil {
				network, address = "udp4", udp4
			}
			if t.debug {
				log.Printf("[BindOutUDP]: %s, BindAddr: %s, Dest: %s\n", network, address, dstAddr)
			}
			return net.ListenPacket(network, address)
		}
		return &delayedUDPConn{connOpener: co}, nil
	}
	server.ListenAndServe(nil)
}

type proxyUDPConn struct {
	UDPConn net.Conn
}

func (p proxyUDPConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, err := p.UDPConn.Read(b)
	if err != nil {
		return 0, nil, err
	}
	d, err := socks5.NewDatagramFromBytes(b[0:n])
	if err != nil {
		return 0, nil, err
	}
	addr, _ := net.ResolveUDPAddr("udp", d.Address())
	n = copy(b, d.Data)
	return n, addr, nil
}

func (uc proxyUDPConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	a, h, p, err := socks5.ParseAddress(addr.String())
	if err != nil {
		return 0, err
	}
	d := socks5.NewDatagram(a, h, p, b)
	b1 := d.Bytes()
	n, err := uc.UDPConn.Write(b1)
	if err != nil {
		return 0, err
	}
	if len(b1) != n {
		return 0, errors.New("not write full")
	}
	return len(b), nil
}
func (uc proxyUDPConn) Close() error                       { return uc.UDPConn.Close() }
func (uc proxyUDPConn) LocalAddr() net.Addr                { return uc.UDPConn.LocalAddr() }
func (uc proxyUDPConn) SetDeadline(t time.Time) error      { return uc.UDPConn.SetDeadline(t) }
func (uc proxyUDPConn) SetReadDeadline(t time.Time) error  { return uc.UDPConn.SetReadDeadline(t) }
func (uc proxyUDPConn) SetWriteDeadline(t time.Time) error { return uc.UDPConn.SetWriteDeadline(t) }
