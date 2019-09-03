# 使用方式

```
$ go get github.com/kplcloud/request
```

### GET

```go
import (
	"fmt"
	"github.com/kplcloud/request"
)

func main(){
	// []byte
	body := request.NewRequest("nsini.com", "GET").Do().Raw()
	fmt.Println("byte", string(body))
	
	
	var resp map[string]interface{}
	err := request.NewRequest("nsini.com", "GET").Do().Into(&resp)
	if err == nil {
		fmt.Println(resp)
	}
}
```

### POST、PUT...

```go
import (
	"fmt"
	"github.com/nsini/request"
)

func main(){
	var resp map[string]interface{}
    err := request.NewRequest("nsini.com", "POST").
    	Body([]byte(`{"hello": "world"}`)).
    	Do().Into(&resp)
    if err == nil {
        fmt.Println(resp)
    }

	fmt.Println(string(res))
}
```

### HttpClient

```go
import (
	"fmt"
	"github.com/nsini/request"
	"crypto/tls"
    "net"
    "net/http"
    "net/url"
	"time"
)

func main(){
	
	var proxy func(r *http.Request) (*url.URL, error)
    proxy = func(_ *http.Request) (*url.URL, error) {
        return url.Parse("http://127.0.0.1:1087")
    }

    dialer := &net.Dialer{
        Timeout:   time.Duration(5 * int64(time.Second)),
        KeepAlive: time.Duration(5 * int64(time.Second)),
    }
	
	var resp map[string]interface{}
    err := request.NewRequest("nsini.com", "POST").
        HttpClient(&http.Client{
            Transport: &http.Transport{
                Proxy: proxy, DialContext: dialer.DialContext,
                TLSClientConfig: &tls.Config{
                    InsecureSkipVerify: false,
                },
            },
        }).Do().Into(&body)
    if err == nil {
        fmt.Println(resp)
    }

	fmt.Println(string(res))
}
```

