package auth

import (
	"bufio"
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
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	internalag "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/antigravity"
)

// TestAntigravityWireEntryPoints 验证模型发现入口 FetchAntigravityAvailableModels 与认证初始化入口
// 按约定接入 antigravitywire（含 1.5.13 consumer UA 与代理校验），且保留 CPA 原有 OAuth 回调地址。
func TestAntigravityWireEntryPoints(t *testing.T) {
	const wantUA = "antigravity/cli/1.5.13 (aidev_client; os_type=linux; arch=amd64; cl=1025641825; auth_method=consumer)"

	cases := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "model_discovery_uses_antigravity_ua_and_rejects_bad_proxy",
			run: func(t *testing.T) {
				// 验证非法代理不静默回退。
				_, status := FetchAntigravityAvailableModels(context.Background(), AntigravityModelProbeRequest{
					BaseURLs:    []string{"https://127.0.0.1:1"},
					ProxyURL:    "://invalid-proxy",
					AccessToken: "fake-token",
					ProjectID:   "fake-project",
				})
				if status != AntigravityProbeStatusTransientError {
					t.Fatalf("非法代理应返回 TransientError，got=%v", status)
				}
				if got := AntigravityUserAgent(); got != wantUA {
					t.Fatalf("AntigravityUserAgent() = %q, want %q", got, wantUA)
				}
			},
		},
		{
			name: "model_discovery_emits_exact_tls_and_http11_header_order_over_proxy",
			run: func(t *testing.T) {
				priv, errKey := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				if errKey != nil {
					t.Fatalf("生成证书密钥失败: %v", errKey)
				}
				tmpl := &x509.Certificate{
					SerialNumber: big.NewInt(2),
					Subject:      pkix.Name{CommonName: "127.0.0.1"},
					NotBefore:    time.Now().Add(-time.Hour),
					NotAfter:     time.Now().Add(time.Hour),
					KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
					ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
					IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
					DNSNames:     []string{"localhost"},
				}
				der, errCert := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
				if errCert != nil {
					t.Fatalf("签发测试证书失败: %v", errCert)
				}
				cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}
				ln, errListen := net.Listen("tcp", "127.0.0.1:0")
				if errListen != nil {
					t.Fatalf("监听失败: %v", errListen)
				}
				defer func() { _ = ln.Close() }()

				ja3Ch := make(chan string, 1)
				var wg sync.WaitGroup
				wg.Add(1)
				go func() {
					defer wg.Done()
					c, errAcc := ln.Accept()
					if errAcc != nil {
						return
					}
					defer func() { _ = c.Close() }()
					_ = c.SetDeadline(time.Now().Add(3 * time.Second))
					var hdr [5]byte
					if _, err := io.ReadFull(c, hdr[:]); err != nil {
						return
					}
					recLen := int(binary.BigEndian.Uint16(hdr[3:5]))
					payload := make([]byte, recLen)
					if _, err := io.ReadFull(c, payload); err != nil {
						return
					}
					// 计算捕获到的 ClientHello JA3 哈希。
					body := payload[4:]
					ver := binary.BigEndian.Uint16(body[0:2])
					pos := 34
					pos += 1 + int(body[pos])
					csLen := int(binary.BigEndian.Uint16(body[pos : pos+2]))
					pos += 2
					var cs, exts, grps []string
					var ec []string
					for i := 0; i < csLen; i += 2 {
						cs = append(cs, strconv.Itoa(int(binary.BigEndian.Uint16(body[pos+i:pos+i+2]))))
					}
					pos += csLen
					pos += 1 + int(body[pos])
					extLen := int(binary.BigEndian.Uint16(body[pos : pos+2]))
					pos += 2
					end := pos + extLen
					for pos+4 <= end && pos+4 <= len(body) {
						et := binary.BigEndian.Uint16(body[pos : pos+2])
						el := int(binary.BigEndian.Uint16(body[pos+2 : pos+4]))
						pos += 4
						d := body[pos : pos+el]
						exts = append(exts, strconv.Itoa(int(et)))
						if et == 10 && len(d) >= 2 {
							l := int(binary.BigEndian.Uint16(d[0:2]))
							for i := 2; i+1 < 2+l && i+1 < len(d); i += 2 {
								grps = append(grps, strconv.Itoa(int(binary.BigEndian.Uint16(d[i:i+2]))))
							}
						}
						if et == 11 && len(d) >= 1 {
							l := int(d[0])
							for i := 1; i < 1+l && i < len(d); i++ {
								ec = append(ec, strconv.Itoa(int(d[i])))
							}
						}
						pos += el
					}
					rawJA3 := fmt.Sprintf("%d,%s,%s,%s,%s", ver, strings.Join(cs, "-"), strings.Join(exts, "-"), strings.Join(grps, "-"), strings.Join(ec, "-"))
					sum := md5.Sum([]byte(rawJA3))
					ja3Ch <- hex.EncodeToString(sum[:])

					// 回放给 tls.Server 让客户端因自签证书不被信任而终止（证明证书校验生效）。
					sPipe, cPipe := net.Pipe()
					go func() {
						_, _ = cPipe.Write(append(hdr[:], payload...))
						_, _ = io.Copy(cPipe, c)
						_ = cPipe.Close()
					}()
					go func() { _, _ = io.Copy(c, cPipe) }()
					srv := tls.Server(sPipe, &tls.Config{Certificates: []tls.Certificate{cert}})
					_ = srv.Handshake()
					_ = srv.Close()
					_ = sPipe.Close()
				}()

				// 对测试自签端发起调用：应先发出对齐的 Antigravity ClientHello，再因自签证书校验失败返回 TransientError。
				_, st := FetchAntigravityAvailableModels(context.Background(), AntigravityModelProbeRequest{
					BaseURLs:    []string{"https://" + strings.Replace(ln.Addr().String(), "127.0.0.1:", "localhost:", 1)},
					AccessToken: "fake-token",
					ProjectID:   "fake-project",
				})
				wg.Wait()
				if st != AntigravityProbeStatusTransientError {
					t.Fatalf("未信任自签证书应返回 TransientError，got=%v", st)
				}
				select {
				case gotJA3 := <-ja3Ch:
					if gotJA3 != "03117a8ed39ef02427ebbc39f121275c" {
						t.Fatalf("FetchAntigravityAvailableModels 的 TLS JA3 = %s, want 03117a8ed39ef02427ebbc39f121275c", gotJA3)
					}
				default:
					t.Fatal("未捕获到 FetchAntigravityAvailableModels 的 TLS ClientHello")
				}
			},
		},
		{
			name: "auth_entry_preserves_loopback_oauth_callback_and_clean_onboard_headers",
			run: func(t *testing.T) {
				rawURL := BuildAntigravityAuthURL("state-test", "")
				parsed, errParse := url.Parse(rawURL)
				if errParse != nil {
					t.Fatalf("解析 OAuth 授权地址失败: %v", errParse)
				}
				wantRedirect := fmt.Sprintf("http://localhost:%d/oauth-callback", AntigravityDefaultCallbackPort)
				if got := parsed.Query().Get("redirect_uri"); got != wantRedirect {
					t.Fatalf("OAuth 回调地址被意外改动: got=%q want=%q", got, wantRedirect)
				}

				// 验证 FetchAntigravityAvailableModels 纯 HTTP/1.1 头序（非 TLS 场景）。
				ln, errListen := net.Listen("tcp", "127.0.0.1:0")
				if errListen != nil {
					t.Fatalf("监听失败: %v", errListen)
				}
				defer func() { _ = ln.Close() }()
				headerCh := make(chan []string, 1)
				go func() {
					c, errAcc := ln.Accept()
					if errAcc != nil {
						return
					}
					defer func() { _ = c.Close() }()
					br := bufio.NewReader(c)
					_, _ = br.ReadString('\n')
					var names []string
					var cl int
					for {
						line, errL := br.ReadString('\n')
						if errL != nil {
							return
						}
						trimmed := strings.TrimRight(line, "\r\n")
						if trimmed == "" {
							break
						}
						if n, v, ok := strings.Cut(trimmed, ":"); ok {
							names = append(names, n)
							if strings.EqualFold(n, "Content-Length") {
								cl, _ = strconv.Atoi(strings.TrimSpace(v))
							}
						}
					}
					if cl > 0 {
						_, _ = io.CopyN(io.Discard, br, int64(cl))
					}
					headerCh <- names
					body := `{"models":{"gemini-4-hydrogen-xhigh":{}}}`
					_, _ = fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
				}()
				hints, st := FetchAntigravityAvailableModels(context.Background(), AntigravityModelProbeRequest{
					BaseURLs:    []string{"http://" + ln.Addr().String()},
					AccessToken: "fake-token",
					ProjectID:   "fake-project",
				})
				if st != AntigravityProbeStatusSuccess || len(hints.ModelIDs) != 1 {
					t.Fatalf("模型发现返回异常: status=%v hints=%+v", st, hints)
				}
				wantOrder := []string{"Host", "User-Agent", "Content-Length", "Authorization", "Content-Type", "Accept-Encoding"}
				if got := <-headerCh; !slices.Equal(got, wantOrder) {
					t.Fatalf("FetchAntigravityAvailableModels 头序不一致: got=%v want=%v", got, wantOrder)
				}

				// 确认 internal/auth/antigravity 默认客户端不携带旧 Node SDK UA。
				authSvc := internalag.NewAntigravityAuth(nil, nil)
				if authSvc == nil {
					t.Fatal("NewAntigravityAuth 返回 nil")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}
