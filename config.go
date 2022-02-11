package wsrec

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jpillora/backoff"
)

// setConnectionState sets state for connectionState
func (rc *RecConn) setConnectionState(state ConnectionState) {
	rc.Lock()
	defer rc.Unlock()

	rc.connectionState = state
}

func (rc *RecConn) setURL(url string) {
	rc.Lock()
	defer rc.Unlock()

	rc.url = url
}

func (rc *RecConn) setReqHeader(reqHeader http.Header) {
	rc.Lock()
	defer rc.Unlock()

	rc.reqHeader = reqHeader
}

func (rc *RecConn) setDefaultRecIntvlMin() {
	rc.Lock()
	defer rc.Unlock()

	if rc.RecIntervalMin == 0 {
		rc.RecIntervalMin = 2 * time.Second
	}
}

func (rc *RecConn) setDefaultRecIntvlMax() {
	rc.Lock()
	defer rc.Unlock()

	if rc.RecIntervalMax == 0 {
		rc.RecIntervalMax = 30 * time.Second
	}
}

func (rc *RecConn) setDefaultRecIntvlFactor() {
	rc.Lock()
	defer rc.Unlock()

	if rc.RecIntervalFactor == 0 {
		rc.RecIntervalFactor = 1.5
	}
}

func (rc *RecConn) setDefaultHandshakeTimeout() {
	rc.Lock()
	defer rc.Unlock()

	if rc.HandshakeTimeout == 0 {
		rc.HandshakeTimeout = 2 * time.Second
	}
}

func (rc *RecConn) setDefaultDialer(handshakeTimeout time.Duration) {
	rc.Lock()
	defer rc.Unlock()

	rc.dialer = &websocket.Dialer{
		HandshakeTimeout: handshakeTimeout,
	}
}

func (rc *RecConn) setDefaultSignal() {
	rc.Lock()
	defer rc.Unlock()
	rc.ready = make(chan bool)
	rc.connecting = make(chan bool, 1)
}

func (rc *RecConn) getHandshakeTimeout() time.Duration {
	rc.RLock()
	defer rc.RUnlock()

	return rc.HandshakeTimeout
}

func (rc *RecConn) getBackoff() *backoff.Backoff {
	rc.RLock()
	defer rc.RUnlock()

	return &backoff.Backoff{
		Min:    rc.RecIntervalMin,
		Max:    rc.RecIntervalMax,
		Factor: rc.RecIntervalFactor,
		Jitter: true,
	}
}

func (rc *RecConn) getKeepAliveTimeout() time.Duration {
	rc.RLock()
	defer rc.RUnlock()

	return rc.KeepAliveTimeout
}

func (rc *RecConn) getConn() *websocket.Conn {
	rc.RLock()
	defer rc.RUnlock()

	return rc.Conn
}

func (rc *RecConn) hasSubscribeHandler() bool {
	rc.RLock()
	defer rc.RUnlock()

	return rc.SubscribeHandler != nil
}

// parseURL parses current url
func (rc *RecConn) parseURL(urlStr string) (string, error) {
	if urlStr == "" {
		return "", errors.New("dial: url cannot be empty")
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return "", errors.New("url: " + err.Error())
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return "", errors.New("url: websocket uris must start with ws or wss scheme")
	}
	if u.User != nil {
		return "", errors.New("url: user name and password are not allowed in websocket URIs")
	}

	return urlStr, nil
}
