package wal

import (
	"bufio"
	"encoding/binary"
	"io"
)

// writeEntry writes a single entry to the WAL
// Format: [LSN:8][OpType:1][DataLen:4][Data:N][Checksum:4][Timestamp:8]
func (w *WAL) writeEntry(entry *Entry) error {
	// Write LSN
	if err := binary.Write(w.writer, binary.LittleEndian, entry.LSN); err != nil {
		return err
	}

	// Write OpType
	if err := w.writer.WriteByte(byte(entry.OpType)); err != nil {
		return err
	}

	// Write data length
	dataLen := uint32(len(entry.Data))
	if err := binary.Write(w.writer, binary.LittleEndian, dataLen); err != nil {
		return err
	}

	// Write data
	if _, err := w.writer.Write(entry.Data); err != nil {
		return err
	}

	// Write checksum
	if err := binary.Write(w.writer, binary.LittleEndian, entry.Checksum); err != nil {
		return err
	}

	// Write timestamp
	if err := binary.Write(w.writer, binary.LittleEndian, entry.Timestamp); err != nil {
		return err
	}

	return nil
}

// readEntry reads a single entry from the reader
func (w *WAL) readEntry(reader *bufio.Reader) (*Entry, error) {
	entry := &Entry{}

	// Read LSN
	if err := binary.Read(reader, binary.LittleEndian, &entry.LSN); err != nil {
		return nil, err
	}

	// Read OpType
	opTypeByte, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	entry.OpType = OpType(opTypeByte)

	// Read data length
	var dataLen uint32
	if err := binary.Read(reader, binary.LittleEndian, &dataLen); err != nil {
		return nil, err
	}

	// Read data
	entry.Data = make([]byte, dataLen)
	if _, err := io.ReadFull(reader, entry.Data); err != nil {
		return nil, err
	}

	// Read checksum
	if err := binary.Read(reader, binary.LittleEndian, &entry.Checksum); err != nil {
		return nil, err
	}

	// Read timestamp
	if err := binary.Read(reader, binary.LittleEndian, &entry.Timestamp); err != nil {
		return nil, err
	}

	return entry, nil
}
