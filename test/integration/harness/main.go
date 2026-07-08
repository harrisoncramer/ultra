//go:build integration

// Command harness is an ultra CLI built for the integration tests. It is the
// real command tree plus a custom "store" secret resolver that reads secrets
// from the Redis instance the tests stand up, exercising the same extension
// path a real consumer uses: register a resolver, then call Execute. It is
// behind the integration build tag so it never ships in the default build.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/harrisoncramer/ultra/cli"

	"github.com/spf13/pflag"
)

// storeResolver resolves an app's secrets from Redis, keyed as "<app>:<name>".
// It speaks just enough RESP to issue a GET, so the resolver stays dependency
// free while still reading from a real store over a real connection.
type storeResolver struct {
	addr string
	app  string
}

// Resolve fetches each requested name from the store, omitting any the store
// doesn't hold.
func (s storeResolver) Resolve(_ context.Context, names []string) (map[string]string, error) {
	out := make(map[string]string, len(names))
	for _, name := range names {
		v, ok, err := redisGet(s.addr, s.app+":"+name)
		if err != nil {
			return nil, fmt.Errorf("store get %q: %w", name, err)
		}
		if ok {
			out[name] = v
		}
	}
	return out, nil
}

// redisGet issues a single RESP GET and returns the value, reporting ok=false
// for a nil (missing) key.
func redisGet(addr, key string) (string, bool, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return "", false, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	req := fmt.Sprintf("*2\r\n$3\r\nGET\r\n$%d\r\n%s\r\n", len(key), key)
	if _, err := conn.Write([]byte(req)); err != nil {
		return "", false, err
	}

	r := bufio.NewReader(conn)
	line, err := r.ReadString('\n')
	if err != nil {
		return "", false, err
	}
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "$") {
		return "", false, fmt.Errorf("unexpected reply %q", line)
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil {
		return "", false, fmt.Errorf("bad bulk length %q: %w", line, err)
	}
	if n < 0 {
		return "", false, nil
	}
	buf := make([]byte, n+2) // value plus trailing CRLF
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", false, err
	}
	return string(buf[:n]), true, nil
}

func init() {
	cli.RegisterSecretResolver(cli.SecretResolverCommand{
		Name:  "store",
		Short: "Integration-test resolver reading secrets from Redis",
		Setup: func(fs *pflag.FlagSet) func(app string) cli.SecretResolver {
			addr := fs.String("store-addr", "", "address of the integration secret store")
			return func(app string) cli.SecretResolver {
				return storeResolver{addr: *addr, app: app}
			}
		},
	})
}

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
