package sshclient

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	mdnsGroupAddr = "224.0.0.251"
	mdnsGroupPort = 5353
	mdnsMaxPacket = 8192
	// unicastResponseBit asks the responder to reply directly to our source
	// port instead of the multicast group, since we query from an ephemeral
	// port rather than the reserved mDNS port 5353 (RFC 6762 Section 5.4).
	unicastResponseBit = 0x8000
)

// looksLikeMDNSHost reports whether host is a ".local" name. Go's net
// package does not implement Multicast DNS (RFC 6762) on any platform, so
// resolving these names requires an explicit mDNS query. Native ssh and ping
// clients resolve them through the operating system's own resolver, which is
// why the same hostname can work outside Vericopy and fail inside it.
func looksLikeMDNSHost(host string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSuffix(host, ".")), ".local")
}

// resolveMDNS resolves a ".local" hostname to an IPv4 address with a single
// one-shot Multicast DNS query.
func resolveMDNS(host string, timeout time.Duration) (net.IP, error) {
	connection, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, fmt.Errorf("could not open mDNS query socket: %w", err)
	}
	defer connection.Close()

	remote, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(mdnsGroupAddr, strconv.Itoa(mdnsGroupPort)))
	if err != nil {
		return nil, err
	}
	return resolveMDNSConn(connection, remote, host, timeout)
}

func resolveMDNSConn(connection *net.UDPConn, remote *net.UDPAddr, host string, timeout time.Duration) (net.IP, error) {
	name := host
	if !strings.HasSuffix(name, ".") {
		name += "."
	}
	query, err := buildMDNSQuery(name)
	if err != nil {
		return nil, err
	}

	if err := connection.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	if _, err := connection.WriteToUDP(query, remote); err != nil {
		return nil, fmt.Errorf("could not send mDNS query: %w", err)
	}

	buffer := make([]byte, mdnsMaxPacket)
	for {
		readCount, _, err := connection.ReadFromUDP(buffer)
		if err != nil {
			return nil, fmt.Errorf("no mDNS response for %q: %w", host, err)
		}
		if ip, ok := parseMDNSReply(buffer[:readCount], name); ok {
			return ip, nil
		}
	}
}

func buildMDNSQuery(name string) ([]byte, error) {
	parsedName, err := dnsmessage.NewName(name)
	if err != nil {
		return nil, fmt.Errorf("invalid mDNS name %q: %w", name, err)
	}
	message := dnsmessage.Message{
		Header: dnsmessage.Header{RecursionDesired: false},
		Questions: []dnsmessage.Question{{
			Name:  parsedName,
			Type:  dnsmessage.TypeA,
			Class: dnsmessage.Class(uint16(dnsmessage.ClassINET) | unicastResponseBit),
		}},
	}
	return message.Pack()
}

// parseMDNSReply extracts the first A-record answer matching name.
func parseMDNSReply(packet []byte, name string) (net.IP, bool) {
	var reply dnsmessage.Message
	if err := reply.Unpack(packet); err != nil {
		return nil, false
	}
	for _, answer := range reply.Answers {
		if !strings.EqualFold(answer.Header.Name.String(), name) {
			continue
		}
		if resource, ok := answer.Body.(*dnsmessage.AResource); ok {
			ip := make(net.IP, net.IPv4len)
			copy(ip, resource.A[:])
			return ip, true
		}
	}
	return nil, false
}
