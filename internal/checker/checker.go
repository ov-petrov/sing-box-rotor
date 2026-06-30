package checker

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

type Result struct {
	CandidateName string
	Latency       time.Duration
	Error         error
}

type Checker struct {
	client *http.Client
	method string
}

func NewWithClient(client *http.Client, method string) *Checker {
	if method == "" {
		method = http.MethodGet
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &Checker{client: client, method: method}
}

func New(proxyURL string, timeout time.Duration, method string) (*Checker, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if method == "" {
		method = http.MethodGet
	}
	tr := &http.Transport{}
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		switch u.Scheme {
		case "http", "https":
			tr.Proxy = http.ProxyURL(u)
		case "socks5":
			tr.DialContext = socks5DialContext(u.Host)
		default:
			return nil, fmt.Errorf("unsupported proxy scheme %q", u.Scheme)
		}
	}
	return NewWithClient(&http.Client{Timeout: timeout, Transport: tr}, method), nil
}

func (c *Checker) Probe(ctx context.Context, testURL string) (time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, c.method, testURL, nil)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	latency := time.Since(start)
	if c.method == http.MethodGet {
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 399 {
		return 0, fmt.Errorf("unexpected status %s", resp.Status)
	}
	return latency, nil
}

func socks5DialContext(proxyAddr string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		var d net.Dialer
		conn, err := d.DialContext(ctx, network, proxyAddr)
		if err != nil {
			return nil, err
		}
		if err := socks5Handshake(conn, addr); err != nil {
			conn.Close()
			return nil, err
		}
		return conn, nil
	}
}

func socks5Handshake(conn net.Conn, target string) error {
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return err
	}
	buf := make([]byte, 262)
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return err
	}
	if buf[0] != 0x05 || buf[1] != 0x00 {
		return fmt.Errorf("socks5 auth rejected")
	}
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		return err
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		return err
	}
	req := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
		req = append(req, 0x01)
		req = append(req, ip.To4()...)
	} else if ip := net.ParseIP(host); ip != nil {
		req = append(req, 0x04)
		req = append(req, ip.To16()...)
	} else {
		req = append(req, 0x03, byte(len(host)))
		req = append(req, []byte(host)...)
	}
	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(port))
	req = append(req, portBytes...)
	if _, err := conn.Write(req); err != nil {
		return err
	}
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		return err
	}
	if buf[1] != 0x00 {
		return fmt.Errorf("socks5 connect failed: code %d", buf[1])
	}
	var skip int
	switch buf[3] {
	case 0x01:
		skip = 4
	case 0x03:
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return err
		}
		skip = int(buf[0])
	case 0x04:
		skip = 16
	default:
		return fmt.Errorf("invalid socks5 address type")
	}
	_, err = io.ReadFull(conn, buf[:skip+2])
	return err
}
