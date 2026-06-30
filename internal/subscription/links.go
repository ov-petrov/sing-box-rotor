package subscription

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type Link struct {
	Type     string
	Tag      string
	Server   string
	Port     int
	UUID     string
	Password string
	Method   string
}

func ParseLink(raw string) (Link, error) {
	switch {
	case strings.HasPrefix(raw, "vmess://"):
		return parseVMess(raw)
	case strings.HasPrefix(raw, "vless://"):
		return parseURLLink(raw, "vless")
	case strings.HasPrefix(raw, "trojan://"):
		return parseURLLink(raw, "trojan")
	case strings.HasPrefix(raw, "ss://"):
		return parseShadowsocks(raw)
	default:
		return Link{}, fmt.Errorf("unsupported proxy link")
	}
}

func parseVMess(raw string) (Link, error) {
	payload := strings.TrimPrefix(raw, "vmess://")
	b, err := base64.RawStdEncoding.DecodeString(payload)
	if err != nil {
		b, err = base64.StdEncoding.DecodeString(payload)
	}
	if err != nil {
		return Link{}, err
	}
	var v struct {
		PS   string `json:"ps"`
		Add  string `json:"add"`
		Port string `json:"port"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return Link{}, err
	}
	port, err := strconv.Atoi(v.Port)
	if err != nil {
		return Link{}, err
	}
	return Link{Type: "vmess", Tag: first(v.PS, "vmess"), Server: v.Add, Port: port, UUID: v.ID}, nil
}

func parseURLLink(raw, typ string) (Link, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Link{}, err
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return Link{}, err
	}
	secret := u.User.Username()
	if typ == "trojan" {
		return Link{Type: typ, Tag: first(strings.TrimPrefix(u.Fragment, "#"), typ), Server: u.Hostname(), Port: port, Password: secret}, nil
	}
	return Link{Type: typ, Tag: first(strings.TrimPrefix(u.Fragment, "#"), typ), Server: u.Hostname(), Port: port, UUID: secret}, nil
}

func parseShadowsocks(raw string) (Link, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Link{}, err
	}
	host := u.Hostname()
	portText := u.Port()
	user := u.User.String()
	if host == "" {
		payload := strings.TrimPrefix(raw, "ss://")
		beforeFrag, frag, _ := strings.Cut(payload, "#")
		decoded, err := base64.RawStdEncoding.DecodeString(beforeFrag)
		if err != nil {
			decoded, err = base64.StdEncoding.DecodeString(beforeFrag)
		}
		if err != nil {
			return Link{}, err
		}
		u, err = url.Parse("ss://" + string(decoded) + "#" + frag)
		if err != nil {
			return Link{}, err
		}
		host, portText, user = u.Hostname(), u.Port(), u.User.String()
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return Link{}, err
	}
	method, password, ok := strings.Cut(user, ":")
	if !ok {
		if decoded, err := base64.RawStdEncoding.DecodeString(user); err == nil {
			method, password, ok = strings.Cut(string(decoded), ":")
		}
	}
	if !ok || net.ParseIP(host) == nil && host == "" {
		return Link{}, fmt.Errorf("invalid shadowsocks link")
	}
	return Link{Type: "shadowsocks", Tag: first(strings.TrimPrefix(u.Fragment, "#"), "ss"), Server: host, Port: port, Method: method, Password: password}, nil
}

func first(v, fallback string) string {
	if v == "" {
		return fallback
	}
	if decoded, err := url.QueryUnescape(v); err == nil {
		return decoded
	}
	return v
}
