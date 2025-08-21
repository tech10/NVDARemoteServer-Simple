package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"time"
)

// Generate a self-signed certificate as long as the server is running.
func serialNumber() *big.Int {
	serialNumLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNum, serialErr := rand.Int(rand.Reader, serialNumLimit)
	if serialErr != nil {
		return big.NewInt(time.Now().UnixNano())
	}

	return serialNum
}

func genCert(writeFile bool) (tls.Certificate, error) {
	blankCert := tls.Certificate{}
	ca := &x509.Certificate{
		SerialNumber: serialNumber(),
		Subject: pkix.Name{
			Country:      []string{"US"},
			Organization: []string{"NVDARemote Server"},
			CommonName:   "Root CA",
		},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:             time.Now().Add(-10 * time.Second),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	priv, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		return blankCert, err
	}
	pubKeyBytes, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	keyID := sha1.Sum(pubKeyBytes)
	ca.SubjectKeyId = keyID[:]
	ca.AuthorityKeyId = keyID[:]
	caBytes, cerr := x509.CreateCertificate(rand.Reader, ca, ca, &priv.PublicKey, priv)
	if cerr != nil {
		return blankCert, cerr
	}

	certPEM := new(bytes.Buffer)
	err = pem.Encode(certPEM, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caBytes,
	})
	if err != nil {
		return blankCert, err
	}

	mpk, merr := x509.MarshalPKCS8PrivateKey(priv)
	if merr != nil {
		return blankCert, merr
	}

	certPrivKeyPEM := new(bytes.Buffer)
	err = pem.Encode(certPrivKeyPEM, &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: mpk,
	})
	if err != nil {
		return blankCert, err
	}

	if writeFile {
		genCertFile(certificatePath, certPEM.Bytes(), certPrivKeyPEM.Bytes())
	}

	return tls.X509KeyPair(certPEM.Bytes(), certPrivKeyPEM.Bytes())
}

func genCertFile(file string, cert, key []byte) {
	log.Printf("Attempting to write certificate to file %s\n", file)
	err := fileRewrite(file, append(key, cert...))
	if err != nil {
		log.Fatalf("Failed to write certificate.\n%s\n", err)
	}
	log.Printf("Certificate and key successfully written to %s\n", file)
}

func fileRewrite(file string, data []byte) error {
	w, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("unable to create or open the file %s\n%w", file, err)
	}
	_, err = w.Write(data)
	if err != nil {
		return fmt.Errorf("unable to write to the file %s\n%w", file, err)
	}
	_ = w.Sync()
	err = w.Close()
	if err != nil {
		return fmt.Errorf("the file at %s encountered an error on close, information may not have been written to it correctly\n%w", file, err)
	}
	return nil
}
