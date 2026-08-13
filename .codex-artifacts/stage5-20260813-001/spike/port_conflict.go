package main
import (
 "fmt"
 "net"
 "github.com/anacrolix/torrent"
)
func main(){
 l,_:=net.Listen("tcp4","127.0.0.1:0"); p:=l.Addr().(*net.TCPAddr).Port; l.Close()
 makeCfg:=func()*torrent.ClientConfig{cfg:=torrent.NewDefaultClientConfig(); cfg.ListenPort=p; cfg.NoDHT=true; cfg.NoDefaultPortForwarding=true; return cfg}
 a,e1:=torrent.NewClient(makeCfg()); if a!=nil {defer a.Close()}
 b,e2:=torrent.NewClient(makeCfg()); if b!=nil {defer b.Close()}
 fmt.Printf("port=%d first=%v second=%v secondClient=%v\n",p,e1,e2,b!=nil)
}
