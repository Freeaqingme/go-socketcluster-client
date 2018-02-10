package scclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var PING = []byte("#1")
var PONG = []byte("#2")

type logger interface {
	Log(msg string)
}

type Client struct {
	EventHandler func(event string, data []byte)
	Logger       logger

	url        string
	conn       *websocket.Conn
	msgCounter uint64

	subscriptions   map[string]func(string, []byte)
	subscriptionsMu *sync.RWMutex

	callbacks  map[uint64]func([]byte)
	callbackMu *sync.Mutex
}

func New(url string) *Client {
	return &Client{
		url:             url,
		callbacks:       make(map[uint64]func([]byte), 0),
		callbackMu:      &sync.Mutex{},
		subscriptions:   make(map[string]func(string, []byte)),
		subscriptionsMu: &sync.RWMutex{},
	}
}

func (c *Client) Connect() error {
	return c.ConnectWithDialer(&websocket.Dialer{})
}

func (c *Client) ConnectWithDialer(dialer *websocket.Dialer) error {
	var err error
	if c.conn, _, err = dialer.Dial(c.url, nil); err != nil {
		return err
	}

	go c.handleIncoming()

	if _, err := c.handshake(); err != nil {
		return err
	}

	return nil
}

type parsedResponse struct {
	Rid   uint64          `json:"rid"`
	Data  json.RawMessage `json:"data"`
	Event string          `json:"event"`
}

func (c *Client) handleIncoming() {
	for {
		msgType, p, err := c.conn.ReadMessage()
		if c.Logger != nil {
			c.Logger.Log(fmt.Sprintf("< %d %s %v", msgType, p, err))
		}

		if err != nil {
			panic(err)
		}

		if msgType == -1 {
			panic("Something's gone wrong!")
		}

		if msgType == websocket.TextMessage && bytes.Equal(p, PING) {
			c.send(PONG)
			continue
		}

		parsedResponse := &parsedResponse{}
		if err := json.Unmarshal(p, parsedResponse); err != nil {
			panic(err)
		}

		if parsedResponse.Rid == 0 {
			go c.handleIncomingEvent(parsedResponse)
			continue
		}

		c.callbackMu.Lock()
		callback := c.callbacks[parsedResponse.Rid]
		if callback != nil {
			delete(c.callbacks, parsedResponse.Rid)
			go callback(parsedResponse.Data)
		}
		c.callbackMu.Unlock()

	}
}

func (c *Client) handleIncomingEvent(event *parsedResponse) {
	if event.Event == "#publish" {
		c.handlePublishEvent(event)
		return
	}

	if c.EventHandler == nil {
		return
	}
	c.EventHandler(event.Event, []byte(event.Data))
}

func (c *Client) Emit(msgType string, data interface{}) (out []byte, outErr error) {
	m := c.newMsg(msgType, data)

	done := make(chan struct{}, 0)

	var once sync.Once
	callback := func(ret []byte) {
		once.Do(func() {
			out = ret
			close(done)
		})
	}

	c.callbackMu.Lock()
	c.callbacks[m.MsgId] = callback
	c.callbackMu.Unlock()

	go func() {
		time.Sleep(5 * time.Second) // Hardcoded timeout, for now
		once.Do(func() {
			outErr = errors.New("no response received before the deadline expired")

			c.callbackMu.Lock()
			delete(c.callbacks, m.MsgId)
			c.callbackMu.Unlock()
			close(done)
		})
	}()

	c.send(m.Serialize())
	<-done

	return out, outErr
}

func (c *Client) EmitWithoutResponse(msgType string, data interface{}) error {
	m := c.newMsg(msgType, data)
	return c.send(m.Serialize())
}

func (c *Client) handshake() (interface{}, error) {
	return c.Emit("#handshake", map[string]*string{"authToken": nil})
}

func (c *Client) Subscribe(ch string, callback func(channel string, data []byte)) error {
	c.subscriptionsMu.Lock()
	c.subscriptions[ch] = callback
	c.subscriptionsMu.Unlock()

	_, err := c.Emit("#subscribe", struct {
		Channel string `json:"channel"`
	}{ch})
	return err
}

type publishEvent struct {
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) handlePublishEvent(event *parsedResponse) {
	publishEvent := &publishEvent{}
	if err := json.Unmarshal(event.Data, publishEvent); err != nil {
		panic(err)
	}

	c.subscriptionsMu.RLock()
	callback, ok := c.subscriptions[publishEvent.Channel]
	c.subscriptionsMu.RUnlock()

	if !ok || callback == nil {
		return
	}

	callback(publishEvent.Channel, []byte(publishEvent.Data))
}

func (c *Client) send(msg []byte) error {
	if c.Logger != nil {
		c.Logger.Log("> " + string(msg))
	}
	return c.conn.WriteMessage(websocket.TextMessage, msg)
}
