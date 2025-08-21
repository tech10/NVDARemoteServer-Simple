package main

import "flag"

var (
	addr              string
	certificatePath   string
	certificateGen    bool
	certificateWrite  bool
	sendOrigin        bool
	motd              string
	motdAlwaysDisplay bool
)

func flags() {
	flag.StringVar(&addr, "addr", ":6837", "Provide the server with a listening address.")
	flag.StringVar(&certificatePath, "cert", "server.pem", "Provide the server with a certificate file to load, containing the private key and certificate in .pem format.")
	flag.BoolVar(&certificateGen, "certgen", false, "Tell the server to automatically generate a certificate. (default false)")
	flag.BoolVar(&certificateWrite, "certgenwrite", true, "Tell the server to write the generated certificate to the file set in -cert. If you do not write the file to -cert and generate it on launch, you will have a different certificate each time the server launches.")
	flag.BoolVar(&sendOrigin, "sendorigin", true, "Tell the server to automatically inject an origin field when sending data to a channel. This is required for braille displays to work correctly.")
	flag.StringVar(&motd, "motd", "", "Provide a message of the day that clients will receive upon joining a channel.")
	flag.BoolVar(&motdAlwaysDisplay, "motdforce", false, "Tell the server to force the message of the day to always display on connected clients when they join a channel. (default false)")
}
