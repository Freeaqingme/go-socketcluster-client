package scclient

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/gorilla/websocket"
)

// Allows for ~3 minutes of 'downtime'
const maxReconnectAttempts = 15

func (c *Client) connect() error {
	var err error
	if c.conn, _, err = c.dialer.Dial(c.url, nil); err != nil {
		return err
	}

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
	c.connMu.Lock()
	err := c.conn.WriteMessage(websocket.TextMessage, msg)
	c.connMu.Unlock()

	if err != nil {
		c.reconnect(0)
		return c.send(msg)
	}

	return nil
}

func (c *Client) receive() (messageType int, p []byte, err error) {
	msgType, p, err := c.conn.ReadMessage()
	c.log(LogLevelDebug, fmt.Sprintf("< %d %s %v", msgType, p, err))

	if err != nil {
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

	if err := c.connect(); err == nil {
		c.log(LogLevelError, "Successfully reconnected!")
		c.resubscribe()
		return
	}

	if attempt >= maxReconnectAttempts {
		c.log(LogLevelError, "Giving up on reconnects")
		panic(fmt.Sprintf(
			"Lost connection with socketcluster, even after %d reconnects :)", attempt,
		))
	}

	c.reconnect(attempt + 1)
}
