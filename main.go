package main

import (
	"os"
)

func main() {
	FlagsInit()

	logger = NewLogger(logLevel)

	certificate, certerr := loadCert()
	if certerr != nil {
		os.Exit(1)
	}

	if !launch {
		logger.Warnf("Launch set to false. This program will successfully exit.\n")
		os.Exit(0)
	}

	server := NewServer(certificate, logger)

	err := server.Start(addr)
	if err != nil {
		os.Exit(1)
	}
}
