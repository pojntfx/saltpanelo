package utils

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"time"
)

const (
	RoleSwitchListener = "switch-listener"
	RoleSwitchClient   = "switch-client"

	RoleAdapterListener = "adapter-listener"
	RoleAdapterClient   = "adapter-client"

	RoleBenchmarkListener = "benchmark-listener"
	RoleBenchmarkClient   = "benchmark-client"
)

func GenerateCertificateAuthority(rsaBits int, validity time.Duration) (*x509.Certificate, []byte, []byte, *rsa.PrivateKey, error) {
	caPrivKey, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		return nil, []byte{}, []byte{}, nil, err
	}

	caCfg := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().Unix()),
		Subject: pkix.Name{
			CommonName: "Saltpanelo Certificate Authority",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(validity),
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	ca, err := x509.CreateCertificate(rand.Reader, caCfg, caCfg, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return nil, []byte{}, []byte{}, nil, err
	}

	var caPEM bytes.Buffer
	if err := pem.Encode(&caPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: ca,
	}); err != nil {
		return nil, []byte{}, []byte{}, nil, err
	}

	var caPrivKeyPEM bytes.Buffer
	if err := pem.Encode(&caPrivKeyPEM, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(caPrivKey),
	}); err != nil {
		return nil, []byte{}, []byte{}, nil, err
	}

	return caCfg, caPEM.Bytes(), caPrivKeyPEM.Bytes(), caPrivKey, nil
}

func GenerateCertificate(rsaBits int, caCfg *x509.Certificate, caPrivKey *rsa.PrivateKey, validity time.Duration, routeID, ip, role string) ([]byte, []byte, error) {
	certPrivKey, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		return []byte{}, []byte{}, err
	}

	ips := []net.IP{}
	if strings.TrimSpace(ip) != "" {
		ips = append(ips, net.ParseIP(ip))
	}

	certCfg := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().Unix()),
		Subject: pkix.Name{
			CommonName: role,
			Country:    []string{routeID},
		},
		IPAddresses: ips,
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(validity),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}

	cert, err := x509.CreateCertificate(rand.Reader, certCfg, caCfg, &certPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return []byte{}, []byte{}, err
	}

	var certPEM bytes.Buffer
	if err := pem.Encode(&certPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert,
	}); err != nil {
		return []byte{}, []byte{}, err
	}

	var certPrivKeyPEM bytes.Buffer
	if err := pem.Encode(&certPrivKeyPEM, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(certPrivKey),
	}); err != nil {
		return []byte{}, []byte{}, err
	}

	return certPEM.Bytes(), certPrivKeyPEM.Bytes(), nil
}
