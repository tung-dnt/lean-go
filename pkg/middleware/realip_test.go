package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tung-dnt/lean-go/pkg/middleware"
)

func TestRealIP(t *testing.T) {
	ip := func(remote string, forwarded ...string) string {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = remote
		for _, f := range forwarded {
			r.Header.Add("X-Forwarded-For", f)
		}

		return middleware.RealIP(r)
	}

	t.Run("with no header it is the address of the connection", func(t *testing.T) {
		assert.Equal(t, "203.0.113.7", ip("203.0.113.7:5000"))
		assert.Equal(t, "2001:db8::1", ip("[2001:db8::1]:5000"))
		assert.Equal(t, "203.0.113.7", ip("203.0.113.7"), "a connection address with no port")
	})

	t.Run("a client that is not behind a proxy of ours cannot choose its address", func(t *testing.T) {
		assert.Equal(t, "203.0.113.7", ip("203.0.113.7:5000", "1.2.3.4"))
		assert.Equal(t, "203.0.113.7", ip("203.0.113.7:5000", "10.0.0.1, 1.2.3.4"))
	})

	t.Run("a proxy on loopback or a private network is believed about the client behind it", func(t *testing.T) {
		for _, proxy := range []string{"127.0.0.1:1", "[::1]:1", "10.1.2.3:1", "172.16.0.9:1", "192.168.1.1:1", "169.254.1.1:1"} {
			assert.Equal(t, "198.51.100.4", ip(proxy, "198.51.100.4"), proxy)
		}
	})

	t.Run("what the nearest proxy appended is the client, not what a client wrote before it", func(t *testing.T) {
		// The client put 1.2.3.4 in the header; the proxy appended the address that it saw.
		assert.Equal(t, "198.51.100.4", ip("10.0.0.2:1", "1.2.3.4, 198.51.100.4"))
		assert.Equal(t, "198.51.100.4", ip("10.0.0.2:1", "1.2.3.4", "198.51.100.4"), "several headers are one list")
	})

	t.Run("proxies behind each other are skipped from the right", func(t *testing.T) {
		assert.Equal(t, "198.51.100.4", ip("10.0.0.2:1", "198.51.100.4, 10.0.0.9, 192.168.0.5"))
		assert.Equal(t, "198.51.100.4", ip("127.0.0.1:1", "[198.51.100.4], 10.0.0.9"))
	})

	t.Run("when every address is a proxy's the first one is it", func(t *testing.T) {
		assert.Equal(t, "10.9.9.9", ip("10.0.0.2:1", "10.9.9.9, 10.0.0.9"))
	})

	t.Run("an entry that is not an address makes the header worth nothing", func(t *testing.T) {
		assert.Equal(t, "10.0.0.2", ip("10.0.0.2:1", "198.51.100.4, unknown"))
		assert.Equal(t, "10.0.0.2", ip("10.0.0.2:1", "garbage"))
	})
}
