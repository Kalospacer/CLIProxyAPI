package antigravitywire

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/md5"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAntigravityWireProfile 验证 Antigravity 出站传输层的 TLS ClientHello、HTTP/1.1 头序、
// 证书校验、代理隧道、显式报错无静默回退、取消语义以及 countTokens 不被误迁移。
func TestAntigravityWireProfile(t *testing.T) {
	// 本地构造带 SAN 的测试自签证书，供隔离 TLS 接收端和证书校验失败场景复用。
	makeTestCert := func(t *testing.T, host string) (tls.Certificate, *x509.CertPool) {
		t.Helper()
		priv, errKey := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if errKey != nil {
			t.Fatalf("生成测试密钥失败: %v", errKey)
		}
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject:      pkix.Name{CommonName: host},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
			DNSNames:     []string{host, "localhost"},
		}
		der, errCert := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
		if errCert != nil {
			t.Fatalf("签发测试证书失败: %v", errCert)
		}
		cert, errParse := x509.ParseCertificate(der)
		if errParse != nil {
			t.Fatalf("解析测试证书失败: %v", errParse)
		}
		pool := x509.NewCertPool()
		pool.AddCert(cert)
		return tls.Certificate{
			Certificate: [][]byte{der},
			PrivateKey:  priv,
			Leaf:        cert,
		}, pool
	}

	// 解析原始 TLS ClientHello 记录，校验固定套件、扩展顺序、曲线、签名算法、ALPN 与 JA3。
	type parsedClientHello struct {
		LegacyVersion   uint16
		CipherSuites    []uint16
		Extensions      []uint16
		SupportedGroups []uint16
		KeyShareGroups  []uint16
		SigAlgs         []uint16
		SigAlgsCert     []uint16
		ALPN            []string
		JA3Hash         string
	}
	parseClientHello := func(raw []byte) (parsedClientHello, error) {
		var out parsedClientHello
		if len(raw) < 9 || raw[0] != 0x16 || raw[5] != 0x01 {
			return out, fmt.Errorf("非法的 TLS ClientHello 记录")
		}
		recLen := int(binary.BigEndian.Uint16(raw[3:5]))
		if len(raw) < 5+recLen {
			return out, fmt.Errorf("ClientHello 长度不完整")
		}
		body := raw[9 : 5+recLen]
		if len(body) < 34 {
			return out, fmt.Errorf("ClientHello 主体过短")
		}
		out.LegacyVersion = binary.BigEndian.Uint16(body[0:2])
		pos := 34
		if pos >= len(body) {
			return out, fmt.Errorf("缺少 session_id 长度")
		}
		sidLen := int(body[pos])
		pos += 1 + sidLen
		if pos+2 > len(body) {
			return out, fmt.Errorf("缺少 cipher_suites 长度")
		}
		csLen := int(binary.BigEndian.Uint16(body[pos : pos+2]))
		pos += 2
		for i := 0; i < csLen; i += 2 {
			out.CipherSuites = append(out.CipherSuites, binary.BigEndian.Uint16(body[pos+i:pos+i+2]))
		}
		pos += csLen
		compLen := int(body[pos])
		pos += 1 + compLen
		if pos+2 > len(body) {
			return out, fmt.Errorf("缺少 extensions 长度")
		}
		extTotal := int(binary.BigEndian.Uint16(body[pos : pos+2]))
		pos += 2
		end := pos + extTotal
		var ecPointFormats []uint8
		for pos+4 <= end && pos+4 <= len(body) {
			extType := binary.BigEndian.Uint16(body[pos : pos+2])
			extLen := int(binary.BigEndian.Uint16(body[pos+2 : pos+4]))
			pos += 4
			if pos+extLen > len(body) {
				break
			}
			data := body[pos : pos+extLen]
			out.Extensions = append(out.Extensions, extType)
			switch extType {
			case 10:
				if len(data) >= 2 {
					l := int(binary.BigEndian.Uint16(data[0:2]))
					for i := 2; i+1 < 2+l && i+1 < len(data); i += 2 {
						out.SupportedGroups = append(out.SupportedGroups, binary.BigEndian.Uint16(data[i:i+2]))
					}
				}
			case 11:
				if len(data) >= 1 {
					l := int(data[0])
					for i := 1; i < 1+l && i < len(data); i++ {
						ecPointFormats = append(ecPointFormats, data[i])
					}
				}
			case 13:
				if len(data) >= 2 {
					l := int(binary.BigEndian.Uint16(data[0:2]))
					for i := 2; i+1 < 2+l && i+1 < len(data); i += 2 {
						out.SigAlgs = append(out.SigAlgs, binary.BigEndian.Uint16(data[i:i+2]))
					}
				}
			case 50:
				if len(data) >= 2 {
					l := int(binary.BigEndian.Uint16(data[0:2]))
					for i := 2; i+1 < 2+l && i+1 < len(data); i += 2 {
						out.SigAlgsCert = append(out.SigAlgsCert, binary.BigEndian.Uint16(data[i:i+2]))
					}
				}
			case 16:
				if len(data) >= 2 {
					l := int(binary.BigEndian.Uint16(data[0:2]))
					p := 2
					for p < 2+l && p < len(data) {
						pl := int(data[p])
						p++
						if p+pl <= len(data) {
							out.ALPN = append(out.ALPN, string(data[p:p+pl]))
						}
						p += pl
					}
				}
			case 51:
				if len(data) >= 2 {
					l := int(binary.BigEndian.Uint16(data[0:2]))
					p := 2
					for p+4 <= 2+l && p+4 <= len(data) {
						grp := binary.BigEndian.Uint16(data[p : p+2])
						kl := int(binary.BigEndian.Uint16(data[p+2 : p+4]))
						out.KeyShareGroups = append(out.KeyShareGroups, grp)
						p += 4 + kl
					}
				}
			}
			pos += extLen
		}
		joinU16 := func(items []uint16) string {
			parts := make([]string, len(items))
			for i, v := range items {
				parts[i] = strconv.Itoa(int(v))
			}
			return strings.Join(parts, "-")
		}
		joinU8 := func(items []uint8) string {
			parts := make([]string, len(items))
			for i, v := range items {
				parts[i] = strconv.Itoa(int(v))
			}
			return strings.Join(parts, "-")
		}
		ja3 := fmt.Sprintf("%d,%s,%s,%s,%s",
			out.LegacyVersion,
			joinU16(out.CipherSuites),
			joinU16(out.Extensions),
			joinU16(out.SupportedGroups),
			joinU8(ecPointFormats),
		)
		sum := md5.Sum([]byte(ja3))
		out.JA3Hash = hex.EncodeToString(sum[:])
		return out, nil
	}

	// 启动一个能同时捕获原始 ClientHello 与原始 HTTP/1.1 请求头顺序的隔离 TLS 接收端。
	type wireCapture struct {
		Hello       parsedClientHello
		HeaderNames []string
		RequestLine string
	}
	startCaptureServer := func(t *testing.T, cert tls.Certificate) (string, <-chan wireCapture, func()) {
		t.Helper()
		ln, errListen := net.Listen("tcp", "127.0.0.1:0")
		if errListen != nil {
			t.Fatalf("监听测试端口失败: %v", errListen)
		}
		ch := make(chan wireCapture, 4)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				rawConn, errAccept := ln.Accept()
				if errAccept != nil {
					return
				}
				go func(c net.Conn) {
					defer func() { _ = c.Close() }()
					_ = c.SetDeadline(time.Now().Add(5 * time.Second))
					var hdr [5]byte
					if _, errRead := io.ReadFull(c, hdr[:]); errRead != nil {
						return
					}
					recLen := int(binary.BigEndian.Uint16(hdr[3:5]))
					payload := make([]byte, recLen)
					if _, errRead := io.ReadFull(c, payload); errRead != nil {
						return
					}
					fullRecord := append(hdr[:], payload...)
					parsed, errParse := parseClientHello(fullRecord)
					if errParse != nil {
						return
					}
					// 通过内存管道把已读取的 ClientHello 字节回放给标准 tls.Server 完成握手。
					serverSide, clientSide := net.Pipe()
					defer func() {
						_ = serverSide.Close()
						_ = clientSide.Close()
					}()
					go func() {
						_, _ = clientSide.Write(fullRecord)
						_, _ = io.Copy(clientSide, c)
						_ = clientSide.Close()
					}()
					go func() {
						_, _ = io.Copy(c, clientSide)
					}()
					tlsSrv := tls.Server(serverSide, &tls.Config{
						Certificates: []tls.Certificate{cert},
						NextProtos:   []string{"h2", "http/1.1"},
					})
					if errHS := tlsSrv.Handshake(); errHS != nil {
						return
					}
					br := bufio.NewReader(tlsSrv)
					reqLine, errLine := br.ReadString('\n')
					if errLine != nil {
						return
					}
					var names []string
					var contentLength int
					var chunked bool
					for {
						line, errHdr := br.ReadString('\n')
						if errHdr != nil {
							return
						}
						trimmed := strings.TrimRight(line, "\r\n")
						if trimmed == "" {
							break
						}
						if name, val, ok := strings.Cut(trimmed, ":"); ok {
							names = append(names, name)
							if strings.EqualFold(name, "Content-Length") {
								contentLength, _ = strconv.Atoi(strings.TrimSpace(val))
							}
							if strings.EqualFold(name, "Transfer-Encoding") && strings.Contains(strings.ToLower(val), "chunked") {
								chunked = true
							}
						}
					}
					if chunked {
						for {
							szLine, errSz := br.ReadString('\n')
							if errSz != nil {
								break
							}
							sz, _ := strconv.ParseInt(strings.TrimSpace(szLine), 16, 64)
							if sz == 0 {
								_, _ = br.ReadString('\n')
								break
							}
							_, _ = io.CopyN(io.Discard, br, sz+2)
						}
					} else if contentLength > 0 {
						_, _ = io.CopyN(io.Discard, br, int64(contentLength))
					}
					ch <- wireCapture{
						Hello:       parsed,
						HeaderNames: names,
						RequestLine: strings.TrimSpace(reqLine),
					}
					_, _ = io.WriteString(tlsSrv, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 2\r\nConnection: close\r\n\r\n{}")
					_ = tlsSrv.Close()
				}(rawConn)
			}
		}()
		return strings.Replace(ln.Addr().String(), "127.0.0.1:", "localhost:", 1), ch, func() {
			_ = ln.Close()
			wg.Wait()
		}
	}

	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "tls_client_hello_and_http11_header_order",
			run: func(t *testing.T) {
				cert, pool := makeTestCert(t, "127.0.0.1")
				addr, ch, stop := startCaptureServer(t, cert)
				defer stop()

				base := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}
				client, errClient := NewClient(OperationInfer, "", base, 5*time.Second)
				if errClient != nil {
					t.Fatalf("NewClient(OperationInfer) 失败: %v", errClient)
				}

				// 使用未知长度 Reader 触发 chunked 传输，校验流式推理头序。
				body := struct{ io.Reader }{bytes.NewReader([]byte(`{"ping":true}`))}
				req, errReq := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://"+addr+"/v1internal:streamGenerateContent?alt=sse", body)
				if errReq != nil {
					t.Fatalf("构造请求失败: %v", errReq)
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer fake-token")
				req.Header.Set("User-Agent", "antigravity/cli/1.5.13 (aidev_client; os_type=linux; arch=amd64; cl=1025641825; auth_method=consumer)")

				resp, errDo := client.Do(req)
				if errDo != nil {
					t.Fatalf("发送请求失败: %v", errDo)
				}
				_ = resp.Body.Close()

				cap := <-ch
				wantCiphers := []uint16{49195, 49199, 49196, 49200, 52393, 52392, 49161, 49171, 49162, 49172, 4865, 4866, 4867}
				wantExts := []uint16{0, 11, 65281, 23, 18, 5, 10, 13, 50, 16, 43, 51}
				wantGroups := []uint16{4588, 4587, 4589, 29, 23, 24, 25}
				wantKeyShares := []uint16{4588, 29}
				wantSigAlgs := []uint16{2308, 2309, 2310, 2052, 1027, 2055, 2053, 2054, 1025, 1281, 1537, 1283, 1539}
				wantSigAlgsCert := []uint16{2308, 2309, 2310, 2052, 1027, 2055, 2053, 2054, 1025, 1281, 1537, 1283, 1539, 513, 515}
				if !slices.Equal(cap.Hello.CipherSuites, wantCiphers) {
					t.Fatalf("CipherSuites 不一致: got=%v want=%v", cap.Hello.CipherSuites, wantCiphers)
				}
				if !slices.Equal(cap.Hello.Extensions, wantExts) {
					t.Fatalf("Extensions 不一致: got=%v want=%v", cap.Hello.Extensions, wantExts)
				}
				if !slices.Equal(cap.Hello.SupportedGroups, wantGroups) {
					t.Fatalf("SupportedGroups 不一致: got=%v want=%v", cap.Hello.SupportedGroups, wantGroups)
				}
				if !slices.Equal(cap.Hello.KeyShareGroups, wantKeyShares) {
					t.Fatalf("KeyShareGroups 不一致: got=%v want=%v", cap.Hello.KeyShareGroups, wantKeyShares)
				}
				if !slices.Equal(cap.Hello.SigAlgs, wantSigAlgs) {
					t.Fatalf("SigAlgs 不一致: got=%v want=%v", cap.Hello.SigAlgs, wantSigAlgs)
				}
				if !slices.Equal(cap.Hello.SigAlgsCert, wantSigAlgsCert) {
					t.Fatalf("SigAlgsCert 不一致: got=%v want=%v", cap.Hello.SigAlgsCert, wantSigAlgsCert)
				}
				if !slices.Equal(cap.Hello.ALPN, []string{"http/1.1"}) {
					t.Fatalf("ALPN 不一致: got=%v want=[http/1.1]", cap.Hello.ALPN)
				}
				if cap.Hello.JA3Hash != "03117a8ed39ef02427ebbc39f121275c" {
					t.Fatalf("JA3 不一致: got=%s want=03117a8ed39ef02427ebbc39f121275c", cap.Hello.JA3Hash)
				}
				wantHeaders := []string{"Host", "User-Agent", "Transfer-Encoding", "Authorization", "Content-Type", "Accept-Encoding"}
				if !slices.Equal(cap.HeaderNames, wantHeaders) {
					t.Fatalf("流式 HTTP/1.1 请求头顺序不一致: got=%v want=%v", cap.HeaderNames, wantHeaders)
				}
			},
		},
		{
			name: "non_stream_header_order_and_cert_verification",
			run: func(t *testing.T) {
				cert, pool := makeTestCert(t, "127.0.0.1")
				addr, ch, stop := startCaptureServer(t, cert)
				defer stop()

				// 1. 无信任根证书时必须拒绝自签证书（不得跳过证书校验）。
				untrustedClient, errUntrusted := NewClient(OperationModelDiscovery, "", &http.Transport{}, 3*time.Second)
				if errUntrusted != nil {
					t.Fatalf("NewClient 失败: %v", errUntrusted)
				}
				reqUntrusted, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://"+addr+"/v1internal:fetchAvailableModels", bytes.NewReader([]byte(`{"project":"p"}`)))
				if resp, errDo := untrustedClient.Do(reqUntrusted); errDo == nil {
					_ = resp.Body.Close()
					t.Fatal("未信任自签证书却握手成功，证书校验被绕过")
				}

				// 2. 信任根证书后校验非流式 Content-Length 头序。
				trustedClient, errTrusted := NewClient(OperationModelDiscovery, "", &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}, 3*time.Second)
				if errTrusted != nil {
					t.Fatalf("NewClient 失败: %v", errTrusted)
				}
				reqTrusted, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://"+addr+"/v1internal:fetchAvailableModels", bytes.NewReader([]byte(`{"project":"p"}`)))
				reqTrusted.Header.Set("Content-Type", "application/json")
				reqTrusted.Header.Set("Authorization", "Bearer fake-token")
				reqTrusted.Header.Set("User-Agent", "antigravity/cli/1.5.13 (aidev_client; os_type=linux; arch=amd64; cl=1025641825; auth_method=consumer)")
				resp, errDo := trustedClient.Do(reqTrusted)
				if errDo != nil {
					t.Fatalf("信任根证书后请求仍失败: %v", errDo)
				}
				_ = resp.Body.Close()

				cap := <-ch
				wantHeaders := []string{"Host", "User-Agent", "Content-Length", "Authorization", "Content-Type", "Accept-Encoding"}
				if !slices.Equal(cap.HeaderNames, wantHeaders) {
					t.Fatalf("非流式 HTTP/1.1 请求头顺序不一致: got=%v want=%v", cap.HeaderNames, wantHeaders)
				}
			},
		},
		{
			name: "invalid_proxy_returns_error_without_fallback",
			run: func(t *testing.T) {
				if _, err := NewClient(OperationInfer, "://bad-proxy", nil, 3*time.Second); err == nil {
					t.Fatal("非法代理地址应显式报错，不得静默回退")
				}
			},
		},
		{
			name: "context_cancellation_aborts_dial",
			run: func(t *testing.T) {
				cert, pool := makeTestCert(t, "127.0.0.1")
				addr, _, stop := startCaptureServer(t, cert)
				defer stop()

				client, errClient := NewClient(OperationInfer, "", &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}, 5*time.Second)
				if errClient != nil {
					t.Fatalf("NewClient 失败: %v", errClient)
				}
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+addr+"/v1internal:streamGenerateContent?alt=sse", bytes.NewReader([]byte(`{}`)))
				if resp, errDo := client.Do(req); errDo == nil {
					_ = resp.Body.Close()
					t.Fatal("已取消的 context 不应完成请求")
				}
			},
		},
		{
			name: "env_https_proxy_honored_with_default_transport",
			run: func(t *testing.T) {
				cert, pool := makeTestCert(t, "127.0.0.1")
				addr, ch, stop := startCaptureServer(t, cert)
				defer stop()

				// 启动本地 CONNECT 代理，确认 DefaultTransport 克隆（含非空 DialContext 与 ProxyFromEnvironment）下仍走 HTTPS_PROXY。
				proxyLn, errLn := net.Listen("tcp", "127.0.0.1:0")
				if errLn != nil {
					t.Fatalf("监听测试代理端口失败: %v", errLn)
				}
				defer func() { _ = proxyLn.Close() }()
				proxySawConnect := make(chan string, 2)
				go func() {
					for {
						c, errAcc := proxyLn.Accept()
						if errAcc != nil {
							return
						}
						go func(clientConn net.Conn) {
							defer func() { _ = clientConn.Close() }()
							br := bufio.NewReader(clientConn)
							line, errLine := br.ReadString(10)
							if errLine != nil {
								return
							}
							for {
								h, errH := br.ReadString(10)
								if errH != nil || strings.TrimSpace(h) == "" {
									break
								}
							}
							fields := strings.Fields(line)
							if len(fields) < 2 || fields[0] != http.MethodConnect {
								return
							}
							proxySawConnect <- fields[1]
							upstream, errUp := net.DialTimeout("tcp", fields[1], 3*time.Second)
							if errUp != nil {
								return
							}
							defer func() { _ = upstream.Close() }()
							_, _ = io.WriteString(clientConn, "HTTP/1.1 200 Connection Established\r\n\r\n")
							done := make(chan struct{}, 2)
							go func() { _, _ = io.Copy(upstream, br); done <- struct{}{} }()
							go func() { _, _ = io.Copy(clientConn, upstream); done <- struct{}{} }()
							<-done
						}(c)
					}
				}()

				proxyParsed, errParseProxy := url.Parse("http://" + proxyLn.Addr().String())
				if errParseProxy != nil {
					t.Fatalf("解析代理地址失败: %v", errParseProxy)
				}

				// 保留 DefaultTransport 的非空 DialContext，同时挂上代理函数（等价于非回环域名的 ProxyFromEnvironment），
				// 验证 wire.go 不会因 base.DialContext != nil 而跳过继承代理。
				base := http.DefaultTransport.(*http.Transport).Clone()
				base.Proxy = http.ProxyURL(proxyParsed)
				base.TLSClientConfig = &tls.Config{RootCAs: pool}
				client, errClient := NewClient(OperationInfer, "", base, 5*time.Second)
				if errClient != nil {
					t.Fatalf("NewClient 失败: %v", errClient)
				}
				req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://"+addr+"/v1internal:streamGenerateContent?alt=sse", bytes.NewReader([]byte(`{}`)))
				resp, errDo := client.Do(req)
				if errDo != nil {
					t.Fatalf("经由 HTTPS_PROXY 发送请求失败: %v", errDo)
				}
				_ = resp.Body.Close()

				select {
				case target := <-proxySawConnect:
					if target != addr {
						t.Fatalf("CONNECT 目标不一致: got=%q want=%q", target, addr)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("未经过 HTTPS_PROXY 建立 CONNECT 隧道")
				}
				cap := <-ch
				if cap.Hello.JA3Hash != "03117a8ed39ef02427ebbc39f121275c" {
					t.Fatalf("经由 HTTPS_PROXY 的 JA3 不一致: got=%s", cap.Hello.JA3Hash)
				}
			},
		},
		{
			name: "count_tokens_keeps_standard_transport",
			run: func(t *testing.T) {
				cert, pool := makeTestCert(t, "127.0.0.1")
				addr, ch, stop := startCaptureServer(t, cert)
				defer stop()

				client, errClient := NewClient(OperationCountTokens, "", &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}, 3*time.Second)
				if errClient != nil {
					t.Fatalf("NewClient(OperationCountTokens) 失败: %v", errClient)
				}
				req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://"+addr+"/v1internal:countTokens", bytes.NewReader([]byte(`{}`)))
				resp, errDo := client.Do(req)
				if errDo != nil {
					t.Fatalf("CountTokens 请求失败: %v", errDo)
				}
				_ = resp.Body.Close()
				cap := <-ch
				if cap.Hello.JA3Hash == "03117a8ed39ef02427ebbc39f121275c" {
					t.Fatalf("OperationCountTokens 不应迁移到 Antigravity uTLS 形态")
				}
			},
		},
		{
			name: "http_connect_proxy_keeps_antigravity_tls_shape",
			run: func(t *testing.T) {
				cert, pool := makeTestCert(t, "127.0.0.1")
				targetAddr, ch, stop := startCaptureServer(t, cert)
				defer stop()

				// 启动隔离 HTTP CONNECT 代理，确认隧道后外发仍使用 Antigravity uTLS ClientHello。
				var sawConnect bool
				proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodConnect {
						http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
						return
					}
					sawConnect = true
					upConn, errDial := net.DialTimeout("tcp", r.Host, 3*time.Second)
					if errDial != nil {
						http.Error(w, errDial.Error(), http.StatusBadGateway)
						return
					}
					hj, ok := w.(http.Hijacker)
					if !ok {
						_ = upConn.Close()
						return
					}
					clientConn, _, errHj := hj.Hijack()
					if errHj != nil {
						_ = upConn.Close()
						return
					}
					_, _ = io.WriteString(clientConn, "HTTP/1.1 200 Connection Established\r\n\r\n")
					go func() {
						_, _ = io.Copy(upConn, clientConn)
						_ = upConn.Close()
					}()
					go func() {
						_, _ = io.Copy(clientConn, upConn)
						_ = clientConn.Close()
					}()
				}))
				defer proxySrv.Close()

				client, errClient := NewClient(OperationQuota, proxySrv.URL, &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}, 5*time.Second)
				if errClient != nil {
					t.Fatalf("NewClient(proxy) 失败: %v", errClient)
				}
				req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://"+targetAddr+"/v1internal:loadCodeAssist", bytes.NewReader([]byte(`{"metadata":{"ideType":"ANTIGRAVITY"}}`)))
				req.Header.Set("Authorization", "Bearer fake-token")
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("User-Agent", "antigravity/cli/1.5.13 (aidev_client; os_type=linux; arch=amd64; cl=1025641825; auth_method=consumer)")
				resp, errDo := client.Do(req)
				if errDo != nil {
					t.Fatalf("经代理请求失败: %v", errDo)
				}
				_ = resp.Body.Close()
				if !sawConnect {
					t.Fatal("未经过 HTTP CONNECT 代理")
				}
				cap := <-ch
				if cap.Hello.JA3Hash != "03117a8ed39ef02427ebbc39f121275c" {
					t.Fatalf("代理隧道后的 JA3 不一致: got=%s", cap.Hello.JA3Hash)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}
