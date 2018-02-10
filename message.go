package scclient

import (
	json "encoding/json"
	"sync/atomic"
)

type msg struct {
	MsgId   uint64      `json:"cid""`
	MsgType string      `json:"event"`
	Data    interface{} `json:"data"`
}

func (c *Client) newMsg(msgType string, data interface{}) *msg {
	return &msg{
		MsgId:   atomic.AddUint64(&c.msgCounter, 1),
		Data:    data,
		MsgType: msgType,
	}
}

func (m *msg) Serialize() []byte {
	jsonBytes, err := json.Marshal(m)
	if err != nil {
		panic("Could not serialize to json: " + err.Error())
	}

	return jsonBytes
}
