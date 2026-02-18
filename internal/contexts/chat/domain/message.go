package domain

type IncomingMessage struct {
	ChatID    string
	Sender    string
	Attr      string
	MsgType   string
	Content   string
	IsGroup   bool
	EventID   string
	RawWho    string
	RawSender string
}
