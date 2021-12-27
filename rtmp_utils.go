// RTMP server

package main

/* Constants */

const N_CHUNK_STREAM = 8
const RTMP_VERSION = 3
const RTMP_HANDSHAKE_SIZE = 1536
const RTMP_HANDSHAKE_UNINIT = 0
const RTMP_HANDSHAKE_0 = 1
const RTMP_HANDSHAKE_1 = 2
const RTMP_HANDSHAKE_2 = 3

const RTMP_PARSE_INIT = 0
const RTMP_PARSE_BASIC_HEADER = 1
const RTMP_PARSE_MESSAGE_HEADER = 2
const RTMP_PARSE_EXTENDED_TIMESTAMP = 3
const RTMP_PARSE_PAYLOAD = 4

const MAX_CHUNK_HEADER = 18

const RTMP_CHUNK_TYPE_0 = 0 // 11-bytes: timestamp(3) + length(3) + stream type(1) + stream id(4)
const RTMP_CHUNK_TYPE_1 = 1 // 7-bytes: delta(3) + length(3) + stream type(1)
const RTMP_CHUNK_TYPE_2 = 2 // 3-bytes: delta(3)
const RTMP_CHUNK_TYPE_3 = 3 // 0-byte

const RTMP_CHANNEL_PROTOCOL = 2
const RTMP_CHANNEL_INVOKE = 3
const RTMP_CHANNEL_AUDIO = 4
const RTMP_CHANNEL_VIDEO = 5
const RTMP_CHANNEL_DATA = 6

var rtmpHeaderSize = []uint32{11, 7, 3, 0}

/* Protocol Control Messages */
const RTMP_TYPE_SET_CHUNK_SIZE = 1
const RTMP_TYPE_ABORT = 2
const RTMP_TYPE_ACKNOWLEDGEMENT = 3             // bytes read report
const RTMP_TYPE_WINDOW_ACKNOWLEDGEMENT_SIZE = 5 // server bandwidth
const RTMP_TYPE_SET_PEER_BANDWIDTH = 6          // client bandwidth

/* User Control Messages Event (4) */
const RTMP_TYPE_EVENT = 4

const RTMP_TYPE_AUDIO = 8
const RTMP_TYPE_VIDEO = 9

/* Data Message */
const RTMP_TYPE_FLEX_STREAM = 15 // AMF3
const RTMP_TYPE_DATA = 18        // AMF0

/* Shared Object Message */
const RTMP_TYPE_FLEX_OBJECT = 16   // AMF3
const RTMP_TYPE_SHARED_OBJECT = 19 // AMF0

/* Command Message */
const RTMP_TYPE_FLEX_MESSAGE = 17 // AMF3
const RTMP_TYPE_INVOKE = 20       // AMF0

/* Aggregate Message */
const RTMP_TYPE_METADATA = 22

const RTMP_CHUNK_SIZE = 128
const RTMP_PING_TIME = 60000
const RTMP_PING_TIMEOUT = 30000

const STREAM_BEGIN = 0x00
const STREAM_EOF = 0x01
const STREAM_DRY = 0x02
const STREAM_EMPTY = 0x1f
const STREAM_READY = 0x20
