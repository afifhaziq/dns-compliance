package dns

import (
	"net"
	"testing"
)

func TestWriteTCPMessageRoundTrip(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	msg := []byte("hello dns")
	go func() {
		if err := writeTCPMessage(client, msg); err != nil {
			t.Errorf("writeTCPMessage: %v", err)
		}
	}()

	got, err := readTCPMessage(server)
	if err != nil {
		t.Fatalf("readTCPMessage: %v", err)
	}
	if string(got) != string(msg) {
		t.Fatalf("got %q, want %q", got, msg)
	}
}

func TestWriteTCPMessageTooLarge(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	err := writeTCPMessage(client, make([]byte, 0x10000))
	if err == nil {
		t.Fatal("expected error for a message too large for the 2-byte length prefix, got nil")
	}
}
