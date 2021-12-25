// FLV Tag

package main

import (
	"encoding/binary"
)

func createFlvTag(packet RTMPPacket) []byte {
	PreviousTagSize := 11 + packet.header.length
	b := make([]byte, PreviousTagSize+4)

	b[0] = byte(packet.header.packet_type)

	aux := make([]byte, 4)
	binary.BigEndian.PutUint32(aux, packet.header.length)
	b[1] = aux[1]
	b[2] = aux[2]
	b[3] = aux[3]

	b[4] = byte(packet.header.timestamp>>16) & 0xff
	b[5] = byte(packet.header.timestamp>>8) & 0xff
	b[6] = byte(packet.header.timestamp) & 0xff
	b[7] = byte(packet.header.timestamp>>24) & 0xff

	b[8] = 0
	b[9] = 0
	b[10] = 0

	aux2 := make([]byte, 4)
	binary.BigEndian.PutUint32(aux2, PreviousTagSize)

	b[PreviousTagSize] = aux2[0]
	b[PreviousTagSize+1] = aux2[1]
	b[PreviousTagSize+2] = aux2[2]
	b[PreviousTagSize+3] = aux2[3]

	for i := uint32(0); i < packet.header.length; i++ {
		b[11+i] = packet.payload[i]
	}

	return b
}
