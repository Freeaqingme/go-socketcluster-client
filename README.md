# Go SocketCluster Client

This library can be used to connect to a [SocketCluster](https://socketcluster.io/#!/) server.

It was written as an alternative to [the official implementation](https://github.com/sacOO7/socketcluster-client-go),
because its API is a 1:1 copy from the JS implementation, whereas this library attempts
to implement a somewhat more Golang-like API.

It uses the [gorilla/websocket](https://github.com/gorilla/websocket) library, so it should
be fully RFC-6455 compliant. Besides that, it reconnects automatically using an exponential
backoff algorithm. It does not, however, implement JSON Web Tokens (JWT), though Pull Requests
are of course welcome :)

## Getting started

```go
import "github.com/Freeaqingme/go-socketcluster-client"

const wsUrl = "wss://sc-02.coinigy.com/socketcluster/"

func main() {
    client := scclient.New(wsUrl)

    // Supply a callback for any events that need to be performed upon every reconnnect
    client.ConnectCallback = func() error {
        _, err := client.Emit("auth", &authEvent{apiKey, apiSecret })
        return err
    }

    if err := client.Connect(); err != nil {
        panic(err)
    }

    channel, err := client.Subscribe("TRADE-KRKN--XBT--EUR")
    if err != nil {
        panic(err)
    }

    go func() {
        for msg := range channel {
            fmt.Println("New kraken trade: " + string(msg))
        }
    }()

    if res, err := client.Emit("exchanges", nil); err != nil {
        panic(err)
    } else {
        fmt.Println("exchanges: " + string(res))
    }
}

```

### Using a custom dialer
Useful if you'd like to use a local proxy or some other special use case
```go
import (
    "github.com/Freeaqingme/go-socketcluster-client"
    "github.com/gorilla/websocket"
)

func main() {
    client := scclient.New(wsUrl)

    uProxy, _ := url.Parse("http://localhost:8888")
    dialer := &websocket.Dialer{
        Proxy: http.ProxyURL(uProxy),
        TLSClientConfig: &tls.Config{
            InsecureSkipVerify: true, // Running this on production is bad, fyi...
        },
    }

    if err := client.ConnectWithDialer(dialer); err != nil {
        panic(err)
    }
}

```

## License

This project is licensed under the Apache2 License - see the [LICENSE](LICENSE) file for details
