package ym

import (
	"encoding/binary"
	"fmt"
)

const maxDepackedSize = 32 << 20 // 32 MiB sanity ceiling for a register dump

// maybeDepack transparently unwraps a single-file LHArc container. YM files are
// conventionally distributed compressed with the -lh5- method; uncompressed
// data (already starting with a YMx! magic) is returned unchanged.
func maybeDepack(data []byte) ([]byte, error) {
	if len(data) < 24 || data[0] == 0 {
		return data, nil
	}
	method := string(data[2:7])
	if len(method) != 5 || method[:3] != "-lh" || method[4] != '-' {
		return data, nil
	}

	headerLen := int(data[0])
	dataOff := headerLen + 2
	if dataOff >= len(data) {
		return nil, fmt.Errorf("ym: truncated LHArc header")
	}
	packedSize := int(binary.LittleEndian.Uint32(data[7:11]))
	origSize := int(binary.LittleEndian.Uint32(data[11:15]))
	if origSize <= 0 || origSize > maxDepackedSize {
		return nil, fmt.Errorf("ym: implausible LHArc original size %d", origSize)
	}

	payload := data[dataOff:]
	if packedSize > 0 && packedSize <= len(payload) {
		payload = payload[:packedSize]
	}

	switch method {
	case "-lh0-", "-lhd-", "-lz4-", "-lz5-", "-pm0-": // stored, no compression
		if origSize > len(payload) {
			return nil, fmt.Errorf("ym: truncated stored LHArc payload")
		}
		out := make([]byte, origSize)
		copy(out, payload)
		return out, nil
	case "-lh4-":
		return lzhDecode(payload, 12, origSize)
	case "-lh5-":
		return lzhDecode(payload, 13, origSize)
	case "-lh6-":
		return lzhDecode(payload, 15, origSize)
	case "-lh7-":
		return lzhDecode(payload, 16, origSize)
	default:
		return nil, fmt.Errorf("ym: unsupported LHArc method %q", method)
	}
}
