package scclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var Ping = []byte("#1")
var Pong = []byte("#2")

type LogLevel int

const (
	LogLevelDebug = iota
	LogLevelError = iota
)

type Client struct {
	EventHandler    func(event string, data []byte)
	ConnectCallback func() error
	Logger          func(level LogLevel, levelmsg string)

	url        string
	conn       *websocket.Conn
	msgCounter uint64
	dialer     *websocket.Dialer

	subscriptions   map[string]chan []byte
	subscriptionsMu *sync.RWMutex

	callbacks  map[uint64]func([]byte)
	callbackMu *sync.Mutex
}

func New(url string) *Client {
	out := &Client{
		url:             url,
		callbacks:       make(map[uint64]func([]byte), 0),
		callbackMu:      &sync.Mutex{},
		subscriptions:   make(map[string]chan []byte),
		subscriptionsMu: &sync.RWMutex{},
	}

	return out
}

func (c *Client) Connect() error {
	return c.ConnectWithDialer(&websocket.Dialer{})
}

func (c *Client) ConnectWithDialer(dialer *websocket.Dialer) error {
	c.dialer = dialer
	return c.connect()
}

type parsedResponse struct {
	Rid   uint64          `json:"rid"`
	Data  json.RawMessage `json:"data"`
	Event string          `json:"event"`
}

func (c *Client) handleIncoming() {
	for {
		msgType, p, err := c.receive()
		if err != nil {
			// Will attempt to reconnect automatically and respawn this receive loop
			return
		}
		if msgType == -1 {
			panic("Something's gone wrong!")
		}

		if msgType == websocket.TextMessage && bytes.Equal(p, Ping) {
			c.send(Pong)
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

func (c *Client) Subscribe(chName string) (<-chan []byte, error) {
	ch := make(chan []byte, 0)
	c.subscriptionsMu.Lock()
	c.subscriptions[chName] = ch
	c.subscriptionsMu.Unlock()

	err := c.subscribe(chName)
	return ch, err
}

func (c *Client) resubscribe() {
	c.subscriptionsMu.Lock()
	for chName := range c.subscriptions {
		c.subscribe(chName)
	}
	c.subscriptionsMu.Unlock()
}

func (c *Client) subscribe(chName string) error {
	_, err := c.Emit("#subscribe", struct {
		Channel string `json:"channel"`
	}{chName})
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
	channel, ok := c.subscriptions[publishEvent.Channel]
	c.subscriptionsMu.RUnlock()

	if !ok {
		return
	}

	channel <- []byte(publishEvent.Data)
}

func (c *Client) log(level LogLevel, msg string) {
	if c.Logger == nil {
		return
	}

	c.Logger(level, msg)
}
