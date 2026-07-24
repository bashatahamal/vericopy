package sshclient

import (
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestLooksLikeMDNSHost(t *testing.T) {
	cases := map[string]bool{
		"bashatahamal-mPC.local":  true,
		"bashatahamal-mPC.local.": true,
		"Example.LOCAL":           true,
		"192.168.100.22":          false,
		"example.com":             false,
		"":                        false,
	}
	for host, want := range cases {
		if got := looksLikeMDNSHost(host); got != want {
			t.Errorf("looksLikeMDNSHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestResolveMDNSConn(t *testing.T) {
	responder, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen responder: %v", err)
	}
	defer responder.Close()

	querier, err := net.ListenUDP("udp4", nil)
	if err != nil {
		t.Fatalf("listen querier: %v", err)
	}
	defer querier.Close()

	wantIP := net.IPv4(192, 168, 100, 22).To4()
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, mdnsMaxPacket)
		_ = responder.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, from, err := responder.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		var query dnsmessage.Message
		if err := query.Unpack(buffer[:n]); err != nil || len(query.Questions) != 1 {
			return
		}
		reply := dnsmessage.Message{
			Header: dnsmessage.Header{Response: true},
			Answers: []dnsmessage.Resource{{
				Header: dnsmessage.ResourceHeader{
					Name:  query.Questions[0].Name,
					Type:  dnsmessage.TypeA,
					Class: dnsmessage.ClassINET,
					TTL:   120,
				},
				Body: &dnsmessage.AResource{A: [4]byte(wantIP)},
			}},
		}
		packet, err := reply.Pack()
		if err != nil {
			return
		}
		_, _ = responder.WriteToUDP(packet, from)
	}()

	ip, err := resolveMDNSConn(querier, responder.LocalAddr().(*net.UDPAddr), "bashatahamal-mPC.local", 2*time.Second)
	<-done
	if err != nil {
		t.Fatalf("resolveMDNSConn returned error: %v", err)
	}
	if !ip.Equal(wantIP) {
		t.Fatalf("resolveMDNSConn returned %v, want %v", ip, wantIP)
	}
}

func TestParseMDNSReplyIgnoresOtherNames(t *testing.T) {
	name, err := dnsmessage.NewName("other-host.local.")
	if err != nil {
		t.Fatalf("NewName: %v", err)
	}
	message := dnsmessage.Message{
		Header: dnsmessage.Header{Response: true},
		Answers: []dnsmessage.Resource{{
			Header: dnsmessage.ResourceHeader{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 120},
			Body:   &dnsmessage.AResource{A: [4]byte{10, 0, 0, 1}},
		}},
	}
	packet, err := message.Pack()
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if _, ok := parseMDNSReply(packet, "bashatahamal-mPC.local."); ok {
		t.Fatal("parseMDNSReply matched an answer for a different name")
	}
}
