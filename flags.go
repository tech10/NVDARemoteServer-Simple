package main

import "flag"

var (
	addr              string
	certificatePath   string
	certificateGen    bool
	sendOrigin        bool
	motd              string
	motdAlwaysDisplay bool
)

func flags() {
	flag.StringVar(&addr, "addr", ":6837", "Provide the server with a listening address.")
	flag.StringVar(&certificatePath, "cert", "server.pem", "Provide the server with a certificate file to load, containing the private key and certificate in .pem format.")
	flag.BoolVar(&certificateGen, "gencert", false, "Allow the server to automatically generate a certificate. (default false)")
	flag.BoolVar(&sendOrigin, "sendorigin", true, "Tell the server to automatically inject an origin field when sending data to a channel.")
	flag.StringVar(&motd, "motd", "", "Provide a message of the day that clients will receive upon joining a channel.")
	flag.BoolVar(&motdAlwaysDisplay, "motdforce", false, "Tell the server to force the message of the day to display on connected clients. (default false)")
}
