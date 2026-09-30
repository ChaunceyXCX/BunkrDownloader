package api

import (
	"encoding/json"

	"github.com/chaunceyxie1/BunkrDownloader/internal/hub"
)

// wsCommand is a client → server control message.
type wsCommand struct {
	Action string `json:"action"`
	TaskID int64  `json:"task_id"`
	All    bool   `json:"all"`
}

// decodeCommand parses a client frame, tolerating unknown fields.
func decodeCommand(raw []byte) (wsCommand, error) {
	var cmd wsCommand
	if len(raw) == 0 {
		return cmd, errEmptyCommand
	}
	if err := json.Unmarshal(raw, &cmd); err != nil {
		return cmd, err
	}
	return cmd, nil
}

type commandError string

func (e commandError) Error() string { return string(e) }

const errEmptyCommand = commandError("empty command")

// marshalFrame serialises a hub frame for the wire.
func marshalFrame(f hub.Frame) ([]byte, error) {
	return json.Marshal(f)
}

// sendFrame is a small helper for direct (non-hub) writes.
func sendFrame(send func([]byte) error, f hub.Frame) error {
	payload, err := marshalFrame(f)
	if err != nil {
		return err
	}
	return send(payload)
}
