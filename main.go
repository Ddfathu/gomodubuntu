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

const (
	WS_MAGIC           = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	TLS_HANDSHAKE_BYTE = 0x16
)

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func main() {
	listenPort := getEnv("PORT", "8080")
	log.Println("================================================================")
	log.Printf("GOLANG TURBO TUNNEL ENGINE ACTIVE ON PORT %s\n", listenPort)
	log.Println("================================================================")
	
	l, err := net.Listen("tcp", ":"+listenPort)
	if err != nil {
		log.Fatalf("Gagal menjalankan listener: %v", err)
	}
	defer l.Close()

	for {
		c, err := l.Accept()
		if err != nil {
			continue
		}
		go handle(c)
	}
}

func handle(c net.Conn) {
	defer c.Close()
	
	// Mode Sabar 2 detik untuk membaca full tumpukan request payload kotor lu
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 65536)
	n, err := c.Read(buf)
	if err != nil || n == 0 {
		return
	}
	c.SetReadDeadline(time.Time{})

	rawPayload := buf[:n]

	// 🛡️ MULTIPLEXING JALUR SSL/TLS MURNI (Stunnel)
	if rawPayload[0] == TLS_HANDSHAKE_BYTE {
		sslTargetHost := getEnv("SSL_TARGET_HOST", "127.0.0.1")
		sslTargetPort := getEnv("SSL_TARGET_PORT", "2443")
		
		targetConn, err := net.Dial("tcp", sslTargetHost+":"+sslTargetPort)
		if err != nil {
			return
		}
		defer targetConn.Close()

		if tcpTarget, ok := targetConn.(*net.TCPConn); ok { tcpTarget.SetNoDelay(true) }
		if tcpClient, ok := c.(*net.TCPConn); ok { tcpClient.SetNoDelay(true) }

		targetConn.Write(rawPayload)
		
		b1 := make([]byte, 65536)
		b2 := make([]byte, 65536)
		go func() {
			for {
				rn, err := c.Read(b1)
				if err != nil { return }
				targetConn.Write(b1[:rn])
			}
		}()
		for {
			rn, err := targetConn.Read(b2)
			if err != nil { return }
			c.Write(b2[:rn])
		}
		return
	}

	// 🌐 JALUR WEBSOCKET HANDSHAKE
	req := string(rawPayload)
	wsKey := ""
	
	for _, line := range strings.Split(req, "\r\n") {
		if strings.Contains(strings.ToLower(line), "sec-websocket-key") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				wsKey = strings.TrimSpace(parts[1])
				break
			}
		}
	}
    
	if wsKey == "" {
		wsKey = base64.StdEncoding.EncodeToString([]byte(time.Now().String() + "turbo-salt"))
	}

	h := sha1.New()
	h.Write([]byte(wsKey + WS_MAGIC))
	accept := base64.StdEncoding.EncodeToString(h.Sum(nil))

	response := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	c.Write([]byte(response))

	// Hubungkan ke Dropbear internal port 22
	wsTargetHost := getEnv("WS_TARGET_HOST", "127.0.0.1")
	wsTargetPort := getEnv("WS_TARGET_PORT", "22")
	ssh, err := net.Dial("tcp", wsTargetHost+":"+wsTargetPort)
	if err != nil {
		return
	}
	defer ssh.Close()

	go ioCopy(ssh, c, true) // Jalur HP -> SSH
	ioCopy(c, ssh, false)   // Jalur SSH -> HP (Dengan Lem Perangko Aman)
}

func ioCopy(dst, src net.Conn, filter bool) {
	b := make([]byte, 65536)
	first := true
	
	if tcpDst, ok := dst.(*net.TCPConn); ok { tcpDst.SetNoDelay(true) }
	if tcpSrc, ok := src.(*net.TCPConn); ok { tcpSrc.SetNoDelay(true) }

	for {
		if !filter {
			// 🔥 Amankan jeda baca ke 15 detik agar tidak dianggap menyerang oleh Cloudflare
			src.SetReadDeadline(time.Now().Add(15 * time.Second))
		}

		n, err := src.Read(b)
		
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() && !filter {
				// Tembak ping sewajarnya tiap 15 detik pas sepi biar jalur tetep hidup
				dst.Write([]byte{0x89, 0x00})
				continue
			}
			return
		}
		
		if n == 0 {
			continue
		}

		if filter && first {
			idx := bytes.Index(b[:n], []byte("SSH-"))
			if idx != -1 { 
				dst.Write(b[idx:n]) 
				first = false 
			}
		} else {
			_, err = dst.Write(b[:n])
			if err != nil {
				return
			}
		}
		
		if !filter {
			src.SetReadDeadline(time.Time{})
		}
	}
}
