# Proxii - HTTP proxy

Sometimes I need a quick HTTP proxy, in the past I used Apache httpd and nginx,
but one needs installation and the other doesn't support CONNECT requests.

Solution, practice some Go language and fix the problem in a simple way. Proxii
is a single file HTTP proxy with support for CONNECT requests and without a
install requirement. When working on remote computers, I can just copy an
executable file and get the proxy functionality working.

It supports normal and transparent proxy, including websockets.

## Usage

Proxii will listen by default on `localhost:8080` you can change it by giving a
parameter:

```sh
> proxii
2018/08/05 13:10:46 Proxii V.0.2.2
2018/08/05 13:10:46 Listen on localhost:8080

# this will listen on all addresses, the ':' is important
> proxii :8080
2018/08/05 13:10:46 Proxii V.0.2.2
2018/08/05 13:10:46 Listen on :8080

# another example
> proxii localhost:12345
2018/08/05 13:10:46 Proxii V.0.2.2
2018/08/05 13:10:46 Listen on localhost:12345
```

