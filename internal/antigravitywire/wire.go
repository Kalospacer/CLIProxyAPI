// Package antigravitywire 实现已核准 Antigravity 出站请求的 TLS 指纹与 HTTP/1.1 头序传输层。
package antigravitywire

import (
	"context"
	stdlibtls "crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	tls "github.com/refraction-networking/utls"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/httpwire"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
	"golang.org/x/net/proxy"
)

// Operation 区分 Antigravity 各类出站操作，避免把 CPA 独有操作（如 countTokens）误迁移到专用形态。
type Operation uint8

const (
	// OperationInfer 表示常规对话或流式推理请求（零值默认）。
	OperationInfer Operation = iota
	// OperationAuthRefresh 表示向 oauth2.googleapis.com 刷新 OAuth 令牌。
	OperationAuthRefresh
	// OperationProjectDiscovery 表示 loadCodeAssist / onboardUser 项目发现请求。
	OperationProjectDiscovery
	// OperationModelDiscovery 表示 fetchAvailableModels 模型目录发现请求。
	OperationModelDiscovery
	// OperationQuota 表示额度与套餐状态查询请求。
	OperationQuota
	// OperationSearch 表示独立联网搜索请求。
	OperationSearch
	// OperationImage 表示图片生成请求。
	OperationImage
	// OperationCountTokens 表示 CPA 独有的 countTokens 请求，不套用 Antigravity uTLS 形态。
	OperationCountTokens
)

func (op Operation) usesAntigravityProfile() bool {
	switch op {
	case OperationInfer,
		OperationAuthRefresh,
		OperationProjectDiscovery,
		OperationModelDiscovery,
		OperationQuota,
		OperationSearch,
		OperationImage:
		return true
	default:
		return false
	}
}

func (op Operation) headerOrder(_, _ string) []string {
	if op == OperationAuthRefresh {
		return []string{
			"Host",
			"User-Agent",
			"Content-Length",
			"Content-Type",
			"Accept-Encoding",
		}
	}
	return []string{
		"Host",
		"User-Agent",
		"Content-Length",
		"Transfer-Encoding",
		"Authorization",
		"Content-Type",
		"X-Goog-User-Project",
		"Accept-Encoding",
	}
}

// dialer 负责单条 Antigravity 出站连接的拨号、uTLS 握手与 HTTP/1.1 请求头排序。
type dialer struct {
	op                 Operation
	proxyDialer        proxy.Dialer
	httpProxyURL       *url.URL
	proxyMode          proxyutil.Mode
	baseProxy          func(*http.Request) (*url.URL, error)
	baseDialContext    func(ctx context.Context, network, addr string) (net.Conn, error)
	rootCAs            *x509.CertPool
	insecureSkipVerify bool
	handshakeTimeout   time.Duration
}

func (d *dialer) dialTCP(ctx context.Context, network, addr string, forTLS bool) (net.Conn, error) {
	if errCtx := ctx.Err(); errCtx != nil {
		return nil, errCtx
	}

	// 明文 HTTP 经由显式或继承 HTTP 代理时，net/http 已把 addr 设为代理地址，直接拨号即可避免二次代理。
	if !forTLS && (d.httpProxyURL != nil || (d.proxyMode == proxyutil.ModeInherit && d.baseProxy != nil)) {
		if d.baseDialContext != nil {
			return d.baseDialContext(ctx, network, addr)
		}
		nd := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		return nd.DialContext(ctx, network, addr)
	}

	activeDialer := d.proxyDialer
	if forTLS && activeDialer == nil && d.proxyMode == proxyutil.ModeInherit && d.baseProxy != nil {
		// 未显式配置代理时，沿用基础 Transport 的代理函数（默认即 ProxyFromEnvironment），
		// 避免设置 DialTLSContext 后在默认 Transport 下静默丢失 HTTPS_PROXY。
		if envProxy, errEnv := d.baseProxy(&http.Request{
			URL: &url.URL{Scheme: "https", Host: addr},
		}); errEnv != nil {
			return nil, fmt.Errorf("antigravitywire: resolve env proxy: %w", errEnv)
		} else if envProxy != nil {
			built, mode, errBuild := proxyutil.BuildDialer(envProxy.String())
			if errBuild != nil {
				return nil, fmt.Errorf("antigravitywire: invalid env proxy: %w", errBuild)
			}
			if mode != proxyutil.ModeInherit && built != nil {
				activeDialer = built
			}
		}
	}

	if activeDialer != nil {
		if ctxDialer, ok := activeDialer.(proxy.ContextDialer); ok {
			return ctxDialer.DialContext(ctx, network, addr)
		}
		return activeDialer.Dial(network, addr)
	}
	if d.baseDialContext != nil {
		return d.baseDialContext(ctx, network, addr)
	}
	nd := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	return nd.DialContext(ctx, network, addr)
}

func (d *dialer) dialPlainContext(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, errDial := d.dialTCP(ctx, network, addr, false)
	if errDial != nil {
		return nil, errDial
	}
	return httpwire.NewOrderedRequestConn(conn, d.op.headerOrder), nil
}

// DialTLSContext 实现 http.Transport 的 TLS 拨号框架回调，复刻官方 Antigravity CLI 1.5.13 ClientHello。
func (d *dialer) DialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	rawConn, errDial := d.dialTCP(ctx, network, addr, true)
	if errDial != nil {
		return nil, fmt.Errorf("antigravitywire: dial upstream: %w", errDial)
	}

	host, _, errSplit := net.SplitHostPort(addr)
	if errSplit != nil {
		_ = rawConn.Close()
		return nil, fmt.Errorf("antigravitywire: split upstream address: %w", errSplit)
	}

	tlsCfg := &tls.Config{
		ServerName:         host,
		RootCAs:            d.rootCAs,
		InsecureSkipVerify: d.insecureSkipVerify,
	}
	uConn := tls.UClient(rawConn, tlsCfg, tls.HelloCustom)
	if errPreset := uConn.ApplyPreset(d.clientHelloSpec()); errPreset != nil {
		_ = uConn.Close()
		return nil, fmt.Errorf("antigravitywire: apply ClientHello spec: %w", errPreset)
	}

	handshakeCtx := ctx
	var cancel context.CancelFunc
	if d.handshakeTimeout > 0 {
		handshakeCtx, cancel = context.WithTimeout(ctx, d.handshakeTimeout)
		defer cancel()
	}

	if errHS := uConn.HandshakeContext(handshakeCtx); errHS != nil {
		_ = uConn.Close()
		if errors.Is(errHS, context.Canceled) || errors.Is(errHS, context.DeadlineExceeded) {
			return nil, fmt.Errorf("antigravitywire: TLS handshake: %w", errHS)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("antigravitywire: TLS handshake: %w", ctxErr)
		}
		return nil, fmt.Errorf("antigravitywire: TLS handshake: %w", errHS)
	}

	return httpwire.NewOrderedRequestConn(uConn, d.op.headerOrder), nil
}

// clientHelloSpec 返回与官方 Antigravity CLI 1.5.13（Linux/amd64 consumer）实测一致的 ClientHello 结构。
func (d *dialer) clientHelloSpec() *tls.ClientHelloSpec {
	return &tls.ClientHelloSpec{
		TLSVersMax: tls.VersionTLS13,
		TLSVersMin: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
		CompressionMethods: []uint8{0},
		Extensions: []tls.TLSExtension{
			&tls.SNIExtension{},
			&tls.SupportedPointsExtension{SupportedPoints: []byte{0}},
			&tls.RenegotiationInfoExtension{Renegotiation: tls.RenegotiateOnceAsClient},
			&tls.ExtendedMasterSecretExtension{},
			&tls.SCTExtension{},
			&tls.StatusRequestExtension{},
			&tls.SupportedCurvesExtension{Curves: []tls.CurveID{
				tls.CurveID(4588), // X25519MLKEM768
				tls.CurveID(4587),
				tls.CurveID(4589),
				tls.X25519,
				tls.CurveP256,
				tls.CurveP384,
				tls.CurveP521,
			}},
			&tls.SignatureAlgorithmsExtension{SupportedSignatureAlgorithms: []tls.SignatureScheme{
				0x0904, 0x0905, 0x0906, // ML-DSA-44 / 65 / 87
				tls.PSSWithSHA256,
				tls.ECDSAWithP256AndSHA256,
				tls.Ed25519,
				tls.PSSWithSHA384,
				tls.PSSWithSHA512,
				tls.PKCS1WithSHA256,
				tls.PKCS1WithSHA384,
				tls.PKCS1WithSHA512,
				tls.ECDSAWithP384AndSHA384,
				tls.ECDSAWithP521AndSHA512,
			}},
			&tls.SignatureAlgorithmsCertExtension{SupportedSignatureAlgorithms: []tls.SignatureScheme{
				0x0904, 0x0905, 0x0906,
				tls.PSSWithSHA256,
				tls.ECDSAWithP256AndSHA256,
				tls.Ed25519,
				tls.PSSWithSHA384,
				tls.PSSWithSHA512,
				tls.PKCS1WithSHA256,
				tls.PKCS1WithSHA384,
				tls.PKCS1WithSHA512,
				tls.ECDSAWithP384AndSHA384,
				tls.ECDSAWithP521AndSHA512,
				tls.PKCS1WithSHA1,
				tls.ECDSAWithSHA1,
			}},
			&tls.ALPNExtension{AlpnProtocols: []string{"http/1.1"}},
			&tls.SupportedVersionsExtension{Versions: []uint16{tls.VersionTLS13, tls.VersionTLS12}},
			&tls.KeyShareExtension{KeyShares: []tls.KeyShare{
				{Group: tls.CurveID(4588)},
				{Group: tls.X25519},
			}},
		},
	}
}

// NewClient 按指定操作构造 Antigravity 出站 HTTP 客户端。
// 已核准操作强制启用 HTTP/1.1、官方 1.5.13 TLS 形态与确定请求头顺序；代理无效时直接报错，不静默回退。
func NewClient(op Operation, proxyURL string, base *http.Transport, timeout time.Duration) (*http.Client, error) {
	proxyURL = strings.TrimSpace(proxyURL)
	if !op.usesAntigravityProfile() {
		var tr *http.Transport
		if base != nil {
			tr = base.Clone()
		} else if def, ok := http.DefaultTransport.(*http.Transport); ok && def != nil {
			tr = def.Clone()
		} else {
			tr = &http.Transport{}
		}
		if proxyURL != "" {
			built, _, errBuild := proxyutil.BuildHTTPTransport(proxyURL)
			if errBuild != nil {
				return nil, fmt.Errorf("antigravitywire: build transport: %w", errBuild)
			}
			if built != nil {
				tr = built
				if base != nil && base.TLSClientConfig != nil {
					tr.TLSClientConfig = base.TLSClientConfig.Clone()
				}
			}
		}
		tr.ForceAttemptHTTP2 = false
		tr.TLSNextProto = make(map[string]func(authority string, c *stdlibtls.Conn) http.RoundTripper)
		return &http.Client{Transport: tr, Timeout: timeout}, nil
	}

	var (
		proxyDialer  proxy.Dialer
		httpProxyURL *url.URL
		proxyMode    = proxyutil.ModeInherit
	)
	if proxyURL != "" {
		setting, errParse := proxyutil.Parse(proxyURL)
		if errParse != nil {
			return nil, fmt.Errorf("antigravitywire: invalid proxy %q: %w", proxyutil.Redact(proxyURL), errParse)
		}
		built, mode, errBuild := proxyutil.BuildDialer(proxyURL)
		if errBuild != nil {
			return nil, fmt.Errorf("antigravitywire: invalid proxy %q: %w", proxyutil.Redact(proxyURL), errBuild)
		}
		proxyMode = mode
		if mode != proxyutil.ModeInherit {
			proxyDialer = built
		}
		if setting.Mode == proxyutil.ModeProxy && setting.URL != nil && (setting.URL.Scheme == "http" || setting.URL.Scheme == "https") {
			httpProxyURL = setting.URL
		}
	}

	var clone *http.Transport
	if base != nil {
		clone = base.Clone()
	} else if def, ok := http.DefaultTransport.(*http.Transport); ok && def != nil {
		clone = def.Clone()
	} else {
		clone = &http.Transport{}
	}

	var rootCAs *x509.CertPool
	var insecureSkipVerify bool
	if clone.TLSClientConfig != nil {
		rootCAs = clone.TLSClientConfig.RootCAs
		insecureSkipVerify = clone.TLSClientConfig.InsecureSkipVerify
	}
	handshakeTimeout := clone.TLSHandshakeTimeout
	if handshakeTimeout <= 0 {
		handshakeTimeout = 10 * time.Second
	}

	baseProxy := clone.Proxy
	wireDialer := &dialer{
		op:                 op,
		proxyDialer:        proxyDialer,
		httpProxyURL:       httpProxyURL,
		proxyMode:          proxyMode,
		baseProxy:          baseProxy,
		baseDialContext:    clone.DialContext,
		rootCAs:            rootCAs,
		insecureSkipVerify: insecureSkipVerify,
		handshakeTimeout:   handshakeTimeout,
	}

	// HTTPS 始终返回 nil 以确保经过 DialTLSContext (uTLS + CONNECT)；明文 HTTP 在显式或继承代理下走标准代理转发。
	if httpProxyURL != nil {
		clone.Proxy = func(req *http.Request) (*url.URL, error) {
			if req != nil && req.URL != nil && strings.EqualFold(req.URL.Scheme, "https") {
				return nil, nil
			}
			return httpProxyURL, nil
		}
	} else if proxyMode == proxyutil.ModeInherit && baseProxy != nil {
		clone.Proxy = func(req *http.Request) (*url.URL, error) {
			if req != nil && req.URL != nil && strings.EqualFold(req.URL.Scheme, "https") {
				return nil, nil
			}
			return baseProxy(req)
		}
	} else {
		clone.Proxy = nil
	}
	clone.ForceAttemptHTTP2 = false
	clone.TLSNextProto = make(map[string]func(authority string, c *stdlibtls.Conn) http.RoundTripper)
	clone.DialContext = wireDialer.dialPlainContext
	clone.DialTLSContext = wireDialer.DialTLSContext

	return &http.Client{
		Transport: clone,
		Timeout:   timeout,
	}, nil
}
