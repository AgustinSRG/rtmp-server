# RTMP Server (Go Implementation)

This is a RTMP (Real Time Messaging Protocol) server for live streaming broadcasting, implemented in Go.

## Compilation

In order to install dependencies, type

```
go get github.com/AgustinSRG/rtmp-server
```

To compile the code type:

```
go build
```

Thhe build command will create a binary in the currenct directory, called `rtmp-server`, or `rtmp-server.exe` if you are using Windows.

## Usage

In order to run the server you have to run the binary. That will run the server in the port `1935`.

The server will accept RTMP connections with the following schema:

```
rtmp://{HOST}/{CHANNEL}/{KEY}
```

Note: Both `CHANNEL` and `KEY` are restricted to letters `A-Z`, numbers `0-9`, dashes `-` and undescores `_`.

By default, it will accept any connections. If you need to restrict the access or customize the server in any way, you can use environment variables.

### RTMP play restrict

You probably only want external users to be able to publish to the RTMP server, since spectartors probably receive the stream using other protocol, like HLS or MPEG-Dash.

In order to do that, set the `RTMP_PLAY_WHITELIST` to a list of allowed internet addresses split by commas. Example: `127.0.0.1,10.0.0.0/8`. You can set IPs, or subnets. It supports both IP version 4 and version 6.

### Event callback

In order to restrict the access and have control over who publishes, the RTMP server can send requests to a remote server with the information of certain events.

Set the `CALLBACK_URL` environment variable to the remote server that is going to handle those events:

 - When an user wants to publish, to validate the atreaming channel and key. (`start`)
 - When a session is closed, meaning the live streaming has ended. (`stop`)

The events are sent as HTTP(S) **POST** requests to the given URL, with empty body, and with a header with name `rtmp-event`, containing the event data encoded as a **Base 64 JWT (JSON Web Token)**, signed using a secret you must provide using the `JWT_SECRET` environment variable.

The JWT contains the following fields:
 - Subject (`sub`) is `rtmp_event`.
 - Event name (`event`) can be `start` or `stop`.
 - Channel (`channel`) is the requested channel to publish.
 - Key (`key`) is the given key to publish.
 - Stream ID (`stream_id`) is the unique ID for the stream session, It is undefined for the `start` event, since is not known yet.
 - Client IP (`client_ip`) is the client IP for logging purposes.

For the `start` event, the event handler server must return with status code **200**, and with a header with name `stream-id`, containing the unique identifier for the RTMP publishing session. If the server does not return with 200, the server will consider the key is invalid and it will close the connection with the client. You can use this to validate streaming keys.

### Redis

This server supports listening for commands using Redis Pub/Sub.

To configure it, set the following variables:

| Variable Name | Description |
|---|---|
| REDIS_USE | Set it to `YES` in order to enable Redis. |
| REDIS_PORT | Port to connect to Redis Pub/Sub. Default is `6379` |
| REDIS_HOST | Host to connect to Redis Pub/Sub. Default is `127.0.0.1` |
| REDIS_PASSWORD | Redis authentication password, if required. |
| REDIS_CHANNEL | Redis channel to listen for commands. |

The commands have the following structure:

```
COMMAND>ARG_1|ARG2|...
```

Each command goes in a separate message.

List of commands:

 - `kill-session>CHANNEL` - Closes any sessions for that specific channel.
 - `close-stream>CHANNEL|STREAM_ID` - Closes specific connection.

These commands are meant to stop a streaming session once started, to enforce application-specific limits.

### TLS

If you want to use TLS, you have to set 3 variables in order for it to work:

| Variable Name | Description |
|---|---|
| SSL_PORT | RTMPS (RTMP over TLS) listening port. Default is `443` |
| SSL_CERT | Path to SSL certificate. |
| SSL_KEY | Path to SSL private key. |

### More options

Here is a list with more options you can configure:

| Variable Name | Description |
|---|---|
| RTMP_PORT | RTMP listening port. Default is `1935` |
| LOG_REQUESTS | Set to `YES` or `NO`. By default is `YES` |
| LOG_DEBUG | Set to `YES` or `NO`. By default is `NO` |
| ID_MAX_LENGTH | Max length for `CHANNEL` and `KEY`. By default is 128 characters |
| MAX_IP_CONCURRENT_CONNECTIONS | Max number of concurrent connections to accept from a single IP. By default is 4. |
| CONCURRENT_LIMIT_WHITELIST | List of IP ranges not affected by the max number of concurrent connections limit. Split by commas. Example: `127.0.0.1,10.0.0.0/8` |

