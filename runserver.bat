@ECHO OFF

SET LOG_DEBUG=YES
SET RTMP_PLAY_WHITELIST=10.0.0.0/8,::1,127.0.0.1
SET RTMP_CHUNK_SIZE=6000
SET RTMP_HOST=127.0.0.1

CALL rtmp-server.exe
PAUSE