package wxbridge

import "encoding/json"

type requestEnvelope struct {
	ID     string      `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params,omitempty"`
}

type responseEnvelope struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type EventEnvelope struct {
	Type  string          `json:"type"`
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type MessageEvent struct {
	EventID   string `json:"event_id"`
	Who       string `json:"who"`
	Sender    string `json:"sender"`
	MsgType   string `json:"msg_type"`
	Attr      string `json:"attr"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

type InitResult struct {
	Nickname string `json:"nickname"`
}
