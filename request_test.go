/**
 * @Time : 2019-07-03 15:37
 * @Author : solacowa@gmail.com
 * @File : request_test
 * @Software: GoLand
 */

package request

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestRequest_Do(t *testing.T) {

	var body = `{"hello": "world"}`

	var res []byte
	err := NewRequest("https://www.iphpt.com/", "POST").
		Body([]byte(body)).Do().Into(&res)
	if err != nil {
		t.Error("err", err.Error())
	}
	t.Log("success", res)
}

func TestRequest_HttpClient(t *testing.T) {
	var proxy func(r *http.Request) (*url.URL, error)
	proxy = func(_ *http.Request) (*url.URL, error) {
		return url.Parse("http://127.0.0.1:1087")
	}

	dialer := &net.Dialer{
		Timeout:   time.Duration(5 * int64(time.Second)),
		KeepAlive: time.Duration(5 * int64(time.Second)),
	}

	var body []byte

	err := NewRequest("www.baidu.com", "GET").
		HttpClient(&http.Client{
			Transport: &http.Transport{
				Proxy: proxy, DialContext: dialer.DialContext,
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: false,
				},
			},
		}).Do().Into(&body)

	if err != nil {
		t.Error("err", err.Error())
	}

	t.Log("body", body)

	var b []byte
	if b, err = NewRequest("https://www.iphpt.com/", "GET").
		Body([]byte(body)).Do().Raw(); err != nil {
		t.Error("raw err", err.Error())
	}

	t.Log("b", string(b))
}
