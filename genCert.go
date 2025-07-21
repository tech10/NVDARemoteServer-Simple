package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"log"
	"math/big"
	"os"
	"time"
)

var (
	certificatePath string
	certificateGen  bool
)

// Generate a self-signed certificate as long as the server is running.
func serialNumber() *big.Int {
	serialNum, serialErr := rand.Int(rand.Reader, big.NewInt(9223372036854775807))
	if serialErr != nil {
		return big.NewInt(time.Now().UnixNano())
	}

	return serialNum
}

func genCert() (tls.Certificate, error) {
	blankCert := tls.Certificate{}
	ca := &x509.Certificate{
		SerialNumber: serialNumber(),
		Subject: pkix.Name{
			Country:      []string{"US"},
			Organization: []string{"NVDARemote Server"},
			CommonName:   "Root CA",
		},
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
		Type:  "EC PRIVATE KEY",
		Bytes: mpk,
	})
	if err != nil {
		return blankCert, err
	}

	genCertFile(certificatePath, certPEM.Bytes(), certPrivKeyPEM.Bytes())

	return tls.X509KeyPair(certPEM.Bytes(), certPrivKeyPEM.Bytes())
}

func genCertFile(file string, cert, key []byte) {
	log.Print("Attempting to write certificate to file " + file + "\n")
	err := fileRewrite(file, append(key, cert...))
	if err != nil {
		log.Fatalf("Failed to write certificate.\n%s\n", err)
	}
	log.Print("Certificate and key successfully written to " + file + "\n")
}

func fileRewrite(file string, data []byte) error {
	w, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return errors.New("Unable to create or open the file " + file + "\n" + err.Error())
	}
	_, err = w.Write(data)
	if err != nil {
		return errors.New("Unable to write to the file " + file + "\n" + err.Error())
	}
	_ = w.Sync()
	err = w.Close()
	if err != nil {
		return errors.New("The file at " + file + " was unable to close. Information may not have been written to it correctly.\n" + err.Error())
	}

	return nil
}
