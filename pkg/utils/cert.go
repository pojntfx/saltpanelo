package utils

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"
)

const (
	rsaBits = 4096
)

func GenerateCertificateAuthority(validity time.Duration) (*x509.Certificate, []byte, []byte, error) {
	caPrivKey, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		return nil, []byte{}, []byte{}, err
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
		return nil, []byte{}, []byte{}, err
	}

	var caPEM bytes.Buffer
	if err := pem.Encode(&caPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: ca,
	}); err != nil {
		return nil, []byte{}, []byte{}, err
	}

	var caPrivKeyPEM bytes.Buffer
	if err := pem.Encode(&caPrivKeyPEM, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(caPrivKey),
	}); err != nil {
		return nil, []byte{}, []byte{}, err
	}

	return caCfg, caPEM.Bytes(), caPrivKeyPEM.Bytes(), nil
}

func GenerateCertificate(caCfg *x509.Certificate, validity time.Duration, laddr, raddr string) ([]byte, []byte, error) {
	certPrivKey, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		return []byte{}, []byte{}, err
	}

	certCfg := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().Unix()),
		Subject: pkix.Name{
			CommonName: "Saltpanelo Certificate",
			Country:    []string{laddr, raddr},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(validity),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}

	cert, err := x509.CreateCertificate(rand.Reader, certCfg, caCfg, &certPrivKey.PublicKey, certPrivKey)
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
