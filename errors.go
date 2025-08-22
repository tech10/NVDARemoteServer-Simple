package main

import "errors"

// ErrNotTCP is returned if the listener is not a TCP listener.
var ErrNotTCP = errors.New("not tcp listener")

// ErrNotTLS is returned if the TLS configuration of the server was nil, and the server cannot be a TLS listener.
var ErrNotTLS = errors.New("not tls listener")
