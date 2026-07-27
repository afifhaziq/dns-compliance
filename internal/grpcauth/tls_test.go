package grpcauth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/afif/dns-tracking/internal/grpcauth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// bothEKU is what every real leaf cert in this system must carry, because
// each binary is both a TLS server and a TLS client.
var bothEKU = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}

// newCA creates a self-signed CA for signing test leaf certs.
func newCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating CA cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing CA cert: %v", err)
	}
	return cert, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// issue signs a leaf cert valid for localhost/127.0.0.1 with the given EKUs.
func issue(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, ekus []x509.ExtKeyUsage) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating leaf key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "test-leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  ekus,
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("creating leaf cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshalling leaf key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

// writePEMs writes the three PEMs to a temp dir and returns their paths.
func writePEMs(t *testing.T, certPEM, keyPEM, caPEM []byte) (certFile, keyFile, caFile string) {
	t.Helper()
	dir := t.TempDir()
	certFile = filepath.Join(dir, "leaf.crt")
	keyFile = filepath.Join(dir, "leaf.key")
	caFile = filepath.Join(dir, "ca.crt")
	for path, data := range map[string][]byte{certFile: certPEM, keyFile: keyPEM, caFile: caPEM} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}
	return certFile, keyFile, caFile
}

// dial stands up a gRPC server with serverCreds, dials it with clientCreds,
// and invokes a method that does not exist. A completed TLS handshake gives
// codes.Unimplemented; a failed one gives a transport error.
func dial(t *testing.T, serverCreds, clientCreds credentials.TransportCredentials) error {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer(grpc.Creds(serverCreds))
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(clientCreds))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return conn.Invoke(ctx, "/grpcauth.test/Ping", &emptypb.Empty{}, &emptypb.Empty{})
}

func TestCreds_AllEmptyMeansPlaintext(t *testing.T) {
	creds, enabled, err := grpcauth.Creds("", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Fatal("enabled=true with no paths set")
	}
	if creds != nil {
		t.Fatal("creds should be nil when disabled")
	}
}

func TestCreds_PartialConfigIsAnError(t *testing.T) {
	cases := []struct{ cert, key, ca string }{
		{"c", "", ""},
		{"", "k", ""},
		{"", "", "a"},
		{"c", "k", ""},
		{"c", "", "a"},
		{"", "k", "a"},
	}
	for _, c := range cases {
		if _, _, err := grpcauth.Creds(c.cert, c.key, c.ca); err == nil {
			t.Fatalf("Creds(%q,%q,%q): want error, got nil", c.cert, c.key, c.ca)
		}
	}
}

func TestCreds_MissingFileIsAnError(t *testing.T) {
	if _, _, err := grpcauth.Creds("/nope.crt", "/nope.key", "/nope.ca"); err == nil {
		t.Fatal("want error for unreadable files, got nil")
	}
}

func TestCreds_AcceptsPeerFromSameCA(t *testing.T) {
	ca, caKey, caPEM := newCA(t)
	certPEM, keyPEM := issue(t, ca, caKey, bothEKU)
	certFile, keyFile, caFile := writePEMs(t, certPEM, keyPEM, caPEM)

	creds, enabled, err := grpcauth.Creds(certFile, keyFile, caFile)
	if err != nil {
		t.Fatalf("Creds: %v", err)
	}
	if !enabled {
		t.Fatal("enabled=false with all three paths set")
	}
	if got := status.Code(dial(t, creds, creds)); got != codes.Unimplemented {
		t.Fatalf("want Unimplemented (handshake succeeded, method absent), got %v", got)
	}
}

// The regression guard for a missing ClientAuth field: without it the server
// accepts any client, silently and with no error.
func TestCreds_RejectsClientFromUnknownCA(t *testing.T) {
	caA, caAKey, caAPEM := newCA(t)
	srvCert, srvKey := issue(t, caA, caAKey, bothEKU)
	srvCertFile, srvKeyFile, caAFile := writePEMs(t, srvCert, srvKey, caAPEM)
	serverCreds, _, err := grpcauth.Creds(srvCertFile, srvKeyFile, caAFile)
	if err != nil {
		t.Fatalf("server Creds: %v", err)
	}

	caB, caBKey, caBPEM := newCA(t)
	cliCert, cliKey := issue(t, caB, caBKey, bothEKU)
	// The rogue client still trusts CA A for the server leg, so only its own
	// certificate is untrusted — isolating ClientAuth verification.
	cliCertFile, cliKeyFile, _ := writePEMs(t, cliCert, cliKey, caBPEM)
	clientCreds, _, err := grpcauth.Creds(cliCertFile, cliKeyFile, caAFile)
	if err != nil {
		t.Fatalf("client Creds: %v", err)
	}

	if got := status.Code(dial(t, serverCreds, clientCreds)); got == codes.Unimplemented {
		t.Fatal("handshake succeeded with a client cert from an untrusted CA — ClientAuth is not enforced")
	}
}

// Documents why gen-mtls-certs.sh must set clientAuth as well as serverAuth.
func TestCreds_RejectsCertMissingClientAuthEKU(t *testing.T) {
	ca, caKey, caPEM := newCA(t)
	srvCert, srvKey := issue(t, ca, caKey, bothEKU)
	srvCertFile, srvKeyFile, caFile := writePEMs(t, srvCert, srvKey, caPEM)
	serverCreds, _, err := grpcauth.Creds(srvCertFile, srvKeyFile, caFile)
	if err != nil {
		t.Fatalf("server Creds: %v", err)
	}

	cliCert, cliKey := issue(t, ca, caKey, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	cliCertFile, cliKeyFile, _ := writePEMs(t, cliCert, cliKey, caPEM)
	clientCreds, _, err := grpcauth.Creds(cliCertFile, cliKeyFile, caFile)
	if err != nil {
		t.Fatalf("client Creds: %v", err)
	}

	if got := status.Code(dial(t, serverCreds, clientCreds)); got == codes.Unimplemented {
		t.Fatal("handshake succeeded with a client cert lacking clientAuth EKU")
	}
}
