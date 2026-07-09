package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

const WS_MAGIC = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists { return value }
	return fallback
}

func main() {
	listenPort := getEnv("PORT", "8080") // Membaca port dinamis dari Railway/Entrypoint
	log.Printf("[Turbo-Go] Engine aktif di port %s -> SSH 22\n", listenPort)
	
	l, _ := net.Listen("tcp", ":"+listenPort)
	for {
		c, _ := l.Accept()
		go handle(c)
	}
}

func handle(c net.Conn) {
	defer c.Close()
	
	// 🔥 KESABARAN EXTRA: Nunggu tumpukan payload kotor lu ngumpul selama 2 detik
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 65536)
	n, err := c.Read(buf)
	if err != nil || n == 0 { return }
	c.SetReadDeadline(time.Time{})

	req := string(buf[:n])
	wsKey := ""
	for _, line := range strings.Split(req, "\r\n") {
		if strings.Contains(strings.ToLower(line), "sec-websocket-key") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 { wsKey = strings.TrimSpace(parts[1]) }
		}
	}
    
	h := sha1.New()
	h.Write([]byte(wsKey + WS_MAGIC))
	accept := base64.StdEncoding.EncodeToString(h.Sum(nil))

	c.Write([]byte("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + "\r\n\r\n"))

	ssh, _ := net.Dial("tcp", "127.0.0.1:22")
	defer ssh.Close()

	go ioCopy(ssh, c, true)  // HP -> SSH (Filter sampah payload)
	ioCopy(c, ssh, false)   // SSH -> HP (Injeksi Heartbeat)
}

func ioCopy(dst, src net.Conn, filter bool) {
	b := make([]byte, 65536)
	first := true
	for {
		n, err := src.Read(b)
		if err != nil { return }
		
		if filter && first {
			idx := bytes.Index(b[:n], []byte("SSH-"))
			if idx != -1 { dst.Write(b[idx:n]); first = false }
		} else {
			dst.Write(b[:n])
		}
		
		if !filter {
			src.SetReadDeadline(time.Now().Add(5 * time.Second))
		}
	}
}
