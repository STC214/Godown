package main

import (
	"fmt"
	"github.com/anacrolix/torrent"
)

func main() {
	c := torrent.NewDefaultClientConfig()
	c.NoDHT = true
	c.DisableTrackers = true
	c.DisableTCP = true
	c.DisableUTP = true
	cl, err := torrent.NewClient(c)
	fmt.Printf("client=%t err=%v\n", cl != nil, err)
	if cl != nil {
		cl.Close()
	}
}
