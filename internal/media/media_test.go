package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"net"
	"strings"
	"testing"
)

func TestDialPinsValidatedAddress(t *testing.T) {
	lookups := 0
	dialed := ""
	lookup := func(context.Context, string, string) ([]net.IP, error) {
		lookups++
		if lookups > 1 {
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		}
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	dial := func(_ context.Context, _ string, address string) (net.Conn, error) {
		dialed = address
		left, right := net.Pipe()
		right.Close()
		return left, nil
	}
	conn, err := dialPublic(context.Background(), "tcp", "attacker.example:443", lookup, dial)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if lookups != 1 || dialed != "93.184.216.34:443" {
		t.Fatalf("DNS result not pinned: lookups=%d dialed=%s", lookups, dialed)
	}
}
func TestDialRejectsMixedPublicPrivateAddresses(t *testing.T) {
	for _, blocked := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "::1", "fe80::1"} {
		t.Run(blocked, func(t *testing.T) {
			dialed := false
			lookup := func(context.Context, string, string) ([]net.IP, error) {
				return []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP(blocked)}, nil
			}
			_, err := dialPublic(context.Background(), "tcp", "attacker.example:80", lookup, func(context.Context, string, string) (net.Conn, error) { dialed = true; return nil, nil })
			if err == nil || dialed {
				t.Fatal("non-public target was dialed")
			}
		})
	}
}
func TestImageDimensionsRejectedBeforeFullDecode(t *testing.T) {
	var file bytes.Buffer
	file.WriteString("\x89PNG\r\n\x1a\n")
	header := make([]byte, 13)
	binary.BigEndian.PutUint32(header, 20000)
	binary.BigEndian.PutUint32(header[4:], 20000)
	header[8] = 8
	header[9] = 2
	binary.Write(&file, binary.BigEndian, uint32(len(header)))
	file.WriteString("IHDR")
	file.Write(header)
	crc := crc32.ChecksumIEEE(append([]byte("IHDR"), header...))
	binary.Write(&file, binary.BigEndian, crc)
	_, err := (&Store{}).SaveBytesContext(context.Background(), file.Bytes(), "upload", 0)
	if err == nil || !strings.Contains(err.Error(), "pixel budget") {
		t.Fatalf("expected early pixel rejection, got %v", err)
	}
}
func TestImageWorkerWaitRespectsCancellation(t *testing.T) {
	imageWorkers <- struct{}{}
	imageWorkers <- struct{}{}
	defer func() { <-imageWorkers; <-imageWorkers }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (&Store{}).SaveBytesContext(ctx, []byte("unused"), "upload", 0)
	if err != context.Canceled {
		t.Fatalf("got %v", err)
	}
}
