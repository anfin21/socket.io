# stdjson

This package is for `encoding/json` support. `encoding/json` has no configuration.

## Usage

```go
import (
    sio "github.com/hhuuson97/socket.io-go"
    jsonparser "github.com/hhuuson97/socket.io-go/parser/json"
    "github.com/hhuuson97/socket.io-go/parser/json/serializer/stdjson"
)

func main() {
    io := sio.NewServer(&sio.ServerConfig{
        ParserCreator: jsonparser.NewCreator(0, stdjson.New()),
    })

    io.Run()
}
```
