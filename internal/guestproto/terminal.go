package guestproto

const TerminalPort uint32 = 1026

// Terminal frames travel only over the actor's VM socket. Data is base64 terminal bytes.
type TerminalFrame struct {
	Type   string  `json:"type"`
	Data   string  `json:"data,omitempty"`
	Cols   uint16  `json:"cols,omitempty"`
	Rows   uint16  `json:"rows,omitempty"`
	Job    *Job    `json:"job,omitempty"`
	Result *Result `json:"result,omitempty"`
	Error  string  `json:"error,omitempty"`
}
