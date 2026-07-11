package sango

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

type Snapshot []byte

var snapshotMagic = []byte("SNGO")

const snapshotVersion = 1

func encodeSnapshot(adapterID string, moduleHash [32]byte, memory []byte) Snapshot {
	buf := bytes.NewBuffer(make([]byte, 0, 4+1+2+len(adapterID)+32+len(memory)))
	buf.Write(snapshotMagic)
	buf.WriteByte(snapshotVersion)
	var idLen [2]byte
	binary.BigEndian.PutUint16(idLen[:], uint16(len(adapterID)))
	buf.Write(idLen[:])
	buf.WriteString(adapterID)
	buf.Write(moduleHash[:])
	buf.Write(memory)
	return Snapshot(buf.Bytes())
}

func decodeSnapshot(s Snapshot) (adapterID string, moduleHash [32]byte, memory []byte, err error) {
	r := s
	if len(r) < 4+1+2 {
		return "", moduleHash, nil, fmt.Errorf("sango: snapshot too short")
	}
	if !bytes.Equal(r[:4], snapshotMagic) {
		return "", moduleHash, nil, fmt.Errorf("sango: invalid snapshot magic")
	}
	if r[4] != snapshotVersion {
		return "", moduleHash, nil, fmt.Errorf("sango: unsupported snapshot version %d", r[4])
	}
	r = r[5:]

	idLen := int(binary.BigEndian.Uint16(r[:2]))
	r = r[2:]
	if len(r) < idLen+32 {
		return "", moduleHash, nil, fmt.Errorf("sango: snapshot header truncated")
	}
	adapterID = string(r[:idLen])
	r = r[idLen:]
	copy(moduleHash[:], r[:32])
	memory = r[32:]
	return adapterID, moduleHash, memory, nil
}
