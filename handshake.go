// RTMP Handshake

package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
)

const MESSAGE_FORMAT_0 = 0
const MESSAGE_FORMAT_1 = 1
const MESSAGE_FORMAT_2 = 2

const RTMP_SIG_SIZE = 1536
const SHA256DL = 32

var RandomCrud = []byte{
	0xf0, 0xee, 0xc2, 0x4a, 0x80, 0x68, 0xbe, 0xe8,
	0x2e, 0x00, 0xd0, 0xd1, 0x02, 0x9e, 0x7e, 0x57,
	0x6e, 0xec, 0x5d, 0x2d, 0x29, 0x80, 0x6f, 0xab,
	0x93, 0xb8, 0xe6, 0x36, 0xcf, 0xeb, 0x31, 0xae,
}

const GenuineFMSConst = "Genuine Adobe Flash Media Server 001"

var GenuineFMSConstCrud = append([]byte(GenuineFMSConst), RandomCrud...)

const GenuineFPConst = "Genuine Adobe Flash Player 001"

// Calculates HMAC
// message - The message
// key - Th key
// Returns the HMAC hash
func calcHmac(message []byte, key []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(message)
	return h.Sum(nil)
}

// Compares two signatures
// sig1 - First signature
// sig2 - Second signature
// Returns true only if the two signatures are the same
func compareSignatures(sig1 []byte, sig2 []byte) bool {
	if len(sig1) != len(sig2) {
		return false
	}

	var result bool

	result = true

	for j := 0; j < len(sig1); j++ {
		result = result && (sig1[j] == sig2[j])
	}

	return result
}

// Gets the basic digest of the RTMP Genuine const of the client
// buf - Buffer to read from
// Returns the digest
func GetClientGenuineConstDigestOffset(buf []byte) uint32 {
	var offset uint32

	offset = uint32(buf[0]) + uint32(buf[1]) + uint32(buf[2]) + uint32(buf[3])
	offset = (offset % 728) + 12

	return offset
}

// Gets the basic digest of the RTMP Genuine const of the server
// buf - Buffer to read from
// Returns the digest
func GetServerGenuineConstDigestOffset(buf []byte) uint32 {
	var offset uint32

	offset = uint32(buf[0]) + uint32(buf[1]) + uint32(buf[2]) + uint32(buf[3])
	offset = (offset % 728) + 776

	return offset
}

// Detects message format from client signature
// clientSig - Client signature
// Returns the message format as an int
func detectClientMessageFormat(clientSig []byte) uint32 {
	var sdl uint32
	var msg []byte
	var aux []byte

	sdl = GetServerGenuineConstDigestOffset(clientSig[772:776])
	msg = make([]byte, sdl)
	copy(msg, clientSig[0:sdl])
	msg = append(msg, clientSig[(sdl+SHA256DL):]...)

	if len(msg) < 1504 {
		aux = make([]byte, 1504-len(msg))
		for j := 0; j < len(aux); j++ {
			aux[j] = 0
		}
		msg = append(msg, aux...)
	} else {
		msg = msg[0:1504]
	}

	var computedSignature []byte
	var providedSignature []byte

	computedSignature = calcHmac(msg, []byte(GenuineFPConst))
	providedSignature = clientSig[sdl:(sdl + SHA256DL)]

	if compareSignatures(computedSignature, providedSignature) {
		return MESSAGE_FORMAT_2
	}

	sdl = GetClientGenuineConstDigestOffset(clientSig[8:12])
	msg = make([]byte, sdl)
	copy(msg, clientSig[0:sdl])
	msg = append(msg, clientSig[(sdl+SHA256DL):]...)

	if len(msg) < 1504 {
		aux = make([]byte, 1504-len(msg))
		for j := 0; j < len(aux); j++ {
			aux[j] = 0
		}
		msg = append(msg, aux...)
	} else {
		msg = msg[0:1504]
	}

	computedSignature = calcHmac(msg, []byte(GenuineFPConst))
	providedSignature = clientSig[sdl:(sdl + SHA256DL)]

	if compareSignatures(computedSignature, providedSignature) {
		return MESSAGE_FORMAT_1
	}

	return MESSAGE_FORMAT_0
}

// Generates the first part of the RTMP server handshake response
// messageFormat - Client message format
// Returns the response
func generateS1(messageFormat uint32) []byte {
	var randomBytes = make([]byte, RTMP_SIG_SIZE-8)
	_, err := rand.Read(randomBytes)

	if err != nil {
		// This should never happen
		panic(err)
	}

	var handshakeBytes []byte
	var msg []byte
	var aux []byte

	handshakeBytes = []byte{
		0, 0, 0, 0, 1, 2, 3, 4,
	}

	handshakeBytes = append(handshakeBytes, randomBytes...)

	if len(handshakeBytes) < RTMP_SIG_SIZE {
		aux = make([]byte, RTMP_SIG_SIZE-len(handshakeBytes))
		for j := 0; j < len(aux); j++ {
			aux[j] = 0
		}
		handshakeBytes = append(handshakeBytes, aux...)
	} else {
		handshakeBytes = handshakeBytes[0:RTMP_SIG_SIZE]
	}

	var serverDigestOffset uint32
	if messageFormat == MESSAGE_FORMAT_1 {
		serverDigestOffset = GetClientGenuineConstDigestOffset(handshakeBytes[8:12])
	} else {
		serverDigestOffset = GetClientGenuineConstDigestOffset(handshakeBytes[772:776])
	}

	msg = make([]byte, serverDigestOffset)
	copy(msg, handshakeBytes[0:serverDigestOffset])
	msg = append(msg, handshakeBytes[(serverDigestOffset+SHA256DL):]...)
	forcedMsgLen := RTMP_SIG_SIZE - SHA256DL

	if len(msg) < forcedMsgLen {
		aux = make([]byte, forcedMsgLen-len(msg))
		for j := 0; j < len(aux); j++ {
			aux[j] = 0
		}
		msg = append(msg, aux...)
	} else {
		msg = msg[0:forcedMsgLen]
	}

	var h = calcHmac(msg, []byte(GenuineFMSConst))

	for j := uint32(0); j < 32; j++ {
		handshakeBytes[serverDigestOffset+j] = h[j]
	}

	return handshakeBytes
}

// Generates the second part of the RTMP server handshake response
// messageFormat - Client message format
// clientSig - Client signature
// Returns the response
func generateS2(messageFormat uint32, clientSig []byte) []byte {
	var randomBytes = make([]byte, RTMP_SIG_SIZE-32)
	_, err := rand.Read(randomBytes)

	if err != nil {
		// This should never happen
		panic(err)
	}

	var challengeKeyOffset uint32

	if messageFormat == MESSAGE_FORMAT_1 {
		challengeKeyOffset = GetClientGenuineConstDigestOffset(clientSig[8:12])
	} else {
		challengeKeyOffset = GetServerGenuineConstDigestOffset(clientSig[772:776])
	}

	var challengeKey = clientSig[challengeKeyOffset:(challengeKeyOffset + 32)]

	var h []byte
	var signature []byte
	var s2Bytes []byte
	var aux []byte

	h = calcHmac(challengeKey, GenuineFMSConstCrud)
	signature = calcHmac(randomBytes, h)

	s2Bytes = append(randomBytes[:], signature...)

	if len(s2Bytes) < RTMP_SIG_SIZE {
		aux = make([]byte, RTMP_SIG_SIZE-len(s2Bytes))
		for j := 0; j < len(aux); j++ {
			aux[j] = 0
		}
		s2Bytes = append(s2Bytes, aux...)
	} else {
		s2Bytes = s2Bytes[0:RTMP_SIG_SIZE]
	}

	return s2Bytes
}

// Generates a RTMP handshake response
// clientSig - Client signature received
// Returns the response to send to the client
func generateS0S1S2(clientSig []byte) []byte {
	var clientType []byte
	var messageFormat uint32
	var allBytes []byte

	clientType = []byte{RTMP_VERSION}
	messageFormat = detectClientMessageFormat(clientSig)

	if messageFormat == MESSAGE_FORMAT_0 {
		LogDebug("Using basic handshake")
		allBytes = append(clientType, clientSig...)
		allBytes = append(allBytes, clientSig...)
	} else {
		LogDebug("Using S1S2 handshake")
		s1 := generateS1(messageFormat)
		s2 := generateS2(messageFormat, clientSig)
		allBytes = append(clientType, s1...)
		allBytes = append(allBytes, s2...)
	}

	return allBytes
}
