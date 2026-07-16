package extensionupdater

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const (
	maxInboundMessageBytes  = 64 << 20
	maxOutboundMessageBytes = 1 << 20
)

type Request struct {
	ProtocolVersion int    `json:"protocol_version"`
	RequestID       string `json:"request_id"`
	Action          string `json:"action"`
	CurrentVersion  string `json:"current_version,omitempty"`
}

type Target struct {
	Browser string `json:"browser"`
	Profile string `json:"profile"`
	Path    string `json:"path"`
	Status  string `json:"status,omitempty"`
}

type ResponseError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

type Response struct {
	ProtocolVersion int            `json:"protocol_version"`
	RequestID       string         `json:"request_id"`
	Kind            string         `json:"kind"`
	OK              bool           `json:"ok"`
	Stage           string         `json:"stage,omitempty"`
	UpdaterVersion  string         `json:"updater_version,omitempty"`
	CurrentVersion  string         `json:"current_version,omitempty"`
	LatestVersion   string         `json:"latest_version,omitempty"`
	UpdateAvailable bool           `json:"update_available,omitempty"`
	Targets         []Target       `json:"targets,omitempty"`
	Error           *ResponseError `json:"error,omitempty"`
}

func ReadMessage(reader io.Reader, target any) error {
	var size uint32
	if err := binary.Read(reader, binary.LittleEndian, &size); err != nil {
		return err
	}
	if size == 0 || size > maxInboundMessageBytes {
		return fmt.Errorf("invalid native message size %d", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return err
	}
	if err := decodeStrictJSON(payload, target); err != nil {
		return fmt.Errorf("decode native message: %w", err)
	}
	return nil
}

func WriteMessage(writer io.Writer, message any) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode native message: %w", err)
	}
	if len(payload) == 0 || len(payload) > maxOutboundMessageBytes {
		return fmt.Errorf("invalid native response size %d", len(payload))
	}
	if err := binary.Write(writer, binary.LittleEndian, uint32(len(payload))); err != nil {
		return err
	}
	_, err = writer.Write(payload)
	return err
}
