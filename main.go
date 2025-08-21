package main

import (
	"crypto/tls"
	"flag"
	"log"
	"os"
)

func main() {
	flags()
	flag.Parse()

	var certificate tls.Certificate
	var certerr error

	if !certificateGen {
		certificate, certerr = tls.LoadX509KeyPair(certificatePath, certificatePath)
	} else {
		certificate, certerr = genCert()
	}

	if certerr != nil {
		log.Fatalf("Certificate loading error: %s\n", certerr)
	}

	server := &Server{
		channels:    make(map[string]Channel),
		Addr:        addr,
		Certificate: certificate,
		Log:         log.New(os.Stdout, "", log.LstdFlags),
	}

	server.Start()
}
