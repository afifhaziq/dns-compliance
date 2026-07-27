package grpcauth

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
)

// Creds builds mutual-TLS transport credentials usable for BOTH dialing and
// listening. crypto/tls ignores whichever fields don't apply to the role it
// is playing, and each binary here is both: the dashboard dials the crawler's
// control service and listens for Submit, and the crawler does the mirror
// image. That is also why every leaf certificate must carry serverAuth and
// clientAuth Extended Key Usage.
//
// enabled is false when all three paths are empty — the plaintext path,
// preserved so existing deployments keep working untouched. Passing only some
// of them is an error, so a half-configured deployment fails at startup
// instead of silently downgrading to plaintext.
func Creds(certFile, keyFile, caFile string) (creds credentials.TransportCredentials, enabled bool, err error) {
	set := 0
	for _, p := range []string{certFile, keyFile, caFile} {
		if p != "" {
			set++
		}
	}
	if set == 0 {
		return nil, false, nil
	}
	if set < 3 {
		return nil, false, fmt.Errorf("mTLS needs all of --tls-cert, --tls-key and --tls-ca, or none of them; got %d of 3", set)
	}

	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, false, fmt.Errorf("loading cert/key: %w", err)
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, false, fmt.Errorf("reading CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, false, fmt.Errorf("no certificates found in %s", caFile)
	}

	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{pair},
		RootCAs:      pool, // verifies the peer when we are the client
		ClientCAs:    pool, // verifies the peer when we are the server
		// Without ClientAuth the server accepts ANY client, with no error and
		// no log line — precisely the failure mutual TLS exists to prevent.
		ClientAuth: tls.RequireAndVerifyClientCert,
		// Both ends are Go binaries we ship together, so there is no
		// compatibility reason to allow anything older.
		MinVersion: tls.VersionTLS13,
	}), true, nil
}
