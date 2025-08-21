package main

func truncate(d []byte, n int) []byte {
	if len(d) <= n {
		return d
	}
	return d[:n]
}
