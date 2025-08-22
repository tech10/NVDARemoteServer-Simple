package main

import (
	"flag"
	"os"
)

func main() {
	flags()
	flag.Parse()

	certificate, certerr := loadCert()
	if certerr != nil {
		logger.Fatalf("Certificate loading error: %s\n", certerr)
	}

	if !launch {
		logger.Printf("Launch set to false. This program will successfully exit.\n")
		os.Exit(0)
	}

	server := NewServer(certificate)

	err := server.Start(addr)
	if err != nil {
		os.Exit(1)
	}
}
