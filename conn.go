package scclient

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type connection struct {
	*websocket.Conn
	startTime   *time.Time
	pingCounter uint64
	msgCounter  uint64

	sendMu    *sync.Mutex // Prevent "concurrent write to websocket connection"
	receiveMu *sync.Mutex
}

// Allows for ~3 minutes of 'downtime'
const maxReconnectAttempts = 15

func (c *Client) connect() error {
	conn, _, err := c.dialer.Dial(c.url, nil)
	if err != nil {
		return err
	}

	startTime := time.Now()
	c.connMu.Lock()
	c.conn = &connection{
		Conn:      conn,
		startTime: &startTime,
		sendMu:    &sync.Mutex{},
		receiveMu: &sync.Mutex{},
	}
	c.connMu.Unlock()

	go c.handleIncoming()

	if _, err := c.handshake(); err != nil {
		return err
	}

	if c.ConnectCallback != nil {
		return c.ConnectCallback()
	}

	return nil
}

func (c *Client) send(msg []byte) error {
	c.log(LogLevelDebug, "> "+string(msg))
	conn := c.getConn()
	conn.sendMu.Lock()
	err := c.getConn().WriteMessage(websocket.TextMessage, msg)
	conn.sendMu.Unlock()

	if err != nil {
		c.reconnect(0)
		return c.send(msg)
	}

	return nil
}

func (c *Client) receive() (messageType int, p []byte, err error) {
	conn := c.getConn()
	conn.receiveMu.Lock()
	msgType, p, err := conn.ReadMessage()
	conn.receiveMu.Unlock()
	c.log(LogLevelDebug, fmt.Sprintf("< %d %s %v", msgType, p, err))

	if err != nil {
		if conn != c.getConn() {
			// We've already reconnected
			return 0, []byte{}, err
		}
		c.reconnect(0)
		return 0, []byte{}, err
	}

	return msgType, p, nil
}

func (c *Client) reconnect(attempt uint) {
	rand.Seed(time.Now().UnixNano())

	skew := (100 + float64(rand.Intn(10))) / 100                                      // Prevent reconnect storm, skew by 10%
	napTime := time.Duration(float64(int(2)<<(attempt-1))*10*skew) * time.Millisecond // Exponential backoff
	c.log(LogLevelError, fmt.Sprintf("Disconnected. Reconnecting in %s", napTime))

	time.Sleep(napTime)

	conn := c.getConn()
	if err := c.connect(); err == nil {
		c.log(LogLevelError, "Successfully reconnected!")
		conn.Close() // Close old connection
		c.resubscribe()
		return
	}

	if attempt >= maxReconnectAttempts {
		c.log(LogLevelError, "Giving up on reconnects")
		panic(fmt.Sprintf(
			"Lost connection with socketcluster, even after %d reconnects :(", attempt,
		))
	}

	c.reconnect(attempt + 1)
}

func (c *Client) getConn() *connection {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	return conn
}

func (c *Client) handlePing() {
	conn := c.getConn()
	atomic.AddUint64(&conn.pingCounter, 1)
	c.send(Pong)

	// Detect timeouts
	go func() {
		startTime := conn.startTime
		counter := atomic.LoadUint64(&conn.pingCounter)

		avgTimeBetweenPongs := time.Duration(time.Since(*startTime).Seconds() / float64(counter))
		timeout := avgTimeBetweenPongs * time.Duration(c.missingPingTimeoutCount) * time.Second

		time.AfterFunc(timeout, func() {
			if counter != atomic.LoadUint64(&conn.pingCounter) {
				return // We've received more pings since
			}

			if conn != c.getConn() {
				return // We've already reconnected
			}

			c.log(LogLevelError, fmt.Sprintf("Haven't received pings in %s, reconnecting...", timeout))
			c.reconnect(0)
		})
	}()
}
